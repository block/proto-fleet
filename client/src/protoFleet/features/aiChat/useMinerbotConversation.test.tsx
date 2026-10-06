import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { useChatStore } from "./useChatStore";
import useMinerbotConversation from "./useMinerbotConversation";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const mocks = vi.hoisted(() => ({ sendMessage: vi.fn(), resolveToolConfirmation: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ chatClient: mocks }));

function signIn(username: string) {
  const auth = useFleetStore.getState().auth;
  auth.setSessionExpiry(new Date(Date.now() + 60_000));
  auth.setUsername(username);
  auth.setIsAuthenticated(true);
}

const wrapper = ({ children }: { children: ReactNode }) => <MemoryRouter>{children}</MemoryRouter>;

describe("Minerbot request boundaries", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useChatStore.getState().clearMessages();
    useChatStore.getState().setStreaming(false);
    signIn("alice");
    mocks.sendMessage.mockImplementation(() => (async function* () {})());
  });

  test("clears all conversation data and rejects late events across logout and login", async () => {
    let release = () => {};
    const released = new Promise<void>((resolve) => {
      release = resolve;
    });
    let finished = () => {};
    const streamFinished = new Promise<void>((resolve) => {
      finished = resolve;
    });
    let signal: AbortSignal | undefined;
    mocks.sendMessage.mockImplementationOnce((_request, options) => {
      signal = options.signal;
      return (async function* () {
        try {
          yield { event: { case: "textDelta", value: { content: "Private fleet observation" } } };
          yield { event: { case: "toolCall", value: { id: "private-call", summary: "Private tool details" } } };
          yield {
            event: {
              case: "confirmationRequired",
              value: {
                confirmationId: "private-approval",
                toolCallId: "private-call",
                title: "Private approval",
                description: "Private target",
                confirmLabel: "Approve",
                details: [],
              },
            },
          };
          await released;
          yield { event: { case: "textDelta", value: { content: "Late private observation" } } };
        } finally {
          finished();
        }
      })();
    });
    const { result } = renderHook(useMinerbotConversation, { wrapper });
    act(() => {
      useChatStore.getState().open();
      void result.current.sendMessage("Alice request");
    });
    await waitFor(() => expect(useChatStore.getState().toolConfirmations).toHaveLength(1));
    act(() => {
      useFleetStore.getState().auth.logout();
    });
    expect(signal?.aborted).toBe(true);
    expect(useChatStore.getState()).toMatchObject({
      messages: [],
      agentActivities: [],
      toolConfirmations: [],
      streamingContent: "",
      streamError: "",
      isStreaming: false,
      isOpen: false,
    });
    act(() => {
      signIn("bob");
    });
    await act(async () => {
      await result.current.sendMessage("Bob request");
      release();
      await streamFinished;
    });
    expect(mocks.sendMessage.mock.calls[1][0].history).toEqual([]);
    expect(useChatStore.getState().messages.map((message) => message.content)).toEqual(["Bob request"]);
    expect(useChatStore.getState().streamingContent).toBe("");
    expect(useChatStore.getState().toolConfirmations).toEqual([]);
  });

  test("rejects a retained prior-session callback before rendering the replacement session", async () => {
    const { result } = renderHook(useMinerbotConversation, { wrapper });
    act(() => {
      useChatStore.getState().addMessage("assistant", "Alice private history");
    });
    const oldSendMessage = result.current.sendMessage;
    await act(async () => {
      useFleetStore.getState().auth.logout();
      signIn("bob");
      await oldSendMessage("Alice queued private prompt");
    });
    expect(mocks.sendMessage).not.toHaveBeenCalled();
    expect(useChatStore.getState().messages).toEqual([]);
    await act(async () => {
      await result.current.sendMessage("Bob prompt");
    });
    expect(mocks.sendMessage).toHaveBeenCalledOnce();
    expect(mocks.sendMessage.mock.calls[0][0]).toMatchObject({ content: "Bob prompt", history: [] });
  });

  test("ignores an old approval response after a replacement session", async () => {
    let release = () => {};
    const resolved = new Promise<void>((resolve) => {
      release = resolve;
    });
    mocks.resolveToolConfirmation.mockImplementation(async () => {
      await resolved;
      return {};
    });
    const { result } = renderHook(useMinerbotConversation, { wrapper });
    const confirmation = {
      id: "approval",
      toolCallId: "call",
      title: "Create site",
      description: "Site details",
      confirmLabel: "Create",
      details: [],
    };
    act(() => {
      useChatStore.getState().addToolConfirmation(confirmation);
    });
    let approval: Promise<void>;
    act(() => {
      approval = result.current.resolveConfirmation(useChatStore.getState().toolConfirmations[0], "approve");
    });
    act(() => {
      signIn("bob");
      useChatStore.getState().clearMessages();
      useChatStore.getState().addToolConfirmation(confirmation);
    });
    await act(async () => {
      release();
      await approval!;
    });
    expect(useChatStore.getState().toolConfirmations[0].status).toBe("pending");
  });

  test("keeps requests valid after fifty history entries and a failed submission", async () => {
    const { result } = renderHook(useMinerbotConversation, { wrapper });
    act(() => {
      for (let index = 0; index < 26; index += 1) {
        useChatStore.getState().addMessage("user", `Question ${index}`);
        useChatStore.getState().addMessage("assistant", `Answer ${index}`);
      }
    });
    mocks.sendMessage.mockImplementationOnce(() => {
      throw new Error("Provider unavailable");
    });
    await act(async () => {
      await result.current.sendMessage("Next question");
    });
    const first = mocks.sendMessage.mock.calls[0][0];
    expect(first.history).toHaveLength(50);
    expect(first.history[0].content).toBe("Question 1");
    expect(first.history.at(-1).content).toBe("Answer 25");
    await act(async () => {
      await result.current.sendMessage("Retry question");
    });
    expect(mocks.sendMessage.mock.calls[1][0].history).toHaveLength(50);
    expect(useChatStore.getState().messages).toHaveLength(54);
  });

  test("bounds provider history content without changing the visible transcript", async () => {
    const { result } = renderHook(useMinerbotConversation, { wrapper });
    const longAnswer = "😀".repeat(33_000);
    act(() => {
      useChatStore.getState().addMessage("assistant", longAnswer);
    });
    await act(async () => {
      await result.current.sendMessage("Continue");
    });
    const content = mocks.sendMessage.mock.calls[0][0].history[0].content;
    expect(Array.from(content)).toHaveLength(32_768);
    expect(content).toMatch(/\[Content truncated\]$/);
    expect(useChatStore.getState().messages[0].content).toBe(longAnswer);
  });
});
