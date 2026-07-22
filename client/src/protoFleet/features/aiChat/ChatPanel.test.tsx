import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import ChatPanel from "./ChatPanel";
import { useChatStore } from "./useChatStore";

const mocks = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  resolveToolConfirmation: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  chatClient: mocks,
}));

describe("ChatPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Element.prototype.scrollIntoView = vi.fn();
    act(() => {
      useChatStore.getState().clearMessages();
      useChatStore.getState().open();
    });
  });

  afterEach(() => {
    act(() => {
      useChatStore.getState().close();
      useChatStore.getState().clearMessages();
    });
  });

  test("shows the live agent empty state without demo activity", () => {
    render(
      <MemoryRouter>
        <ChatPanel />
      </MemoryRouter>,
    );

    expect(screen.getByText("Beta")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "What would you like to know?" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Summarize fleet health" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show configured mining pools" })).toBeInTheDocument();
    expect(screen.queryByText("Demo data")).not.toBeInTheDocument();
    expect(screen.queryByText("RebootBot")).not.toBeInTheDocument();
  });

  test("renders assistant data tables as structured content", () => {
    act(() => {
      useChatStore.getState().addMessage("assistant", "| Miner state | Count |\n| --- | ---: |\n| Offline | 14 |");
    });

    render(
      <MemoryRouter>
        <ChatPanel />
      </MemoryRouter>,
    );

    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Miner state" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Offline" })).toBeInTheDocument();
  });

  test("places compact tool statuses between the request and response", () => {
    act(() => {
      useChatStore.getState().addMessage("user", "Summarize fleet health");
      useChatStore.getState().beginToolActivity("call-1", "Checking fleet health");
      useChatStore.getState().finishToolActivity("call-1", true, "Read state for 14 miners");
      useChatStore.getState().addMessage("assistant", "All 14 miners are offline.");
    });

    render(
      <MemoryRouter>
        <ChatPanel />
      </MemoryRouter>,
    );

    const transcript = screen.getByLabelText("Conversation").textContent ?? "";
    const requestIndex = transcript.indexOf("Summarize fleet health");
    const statusIndex = transcript.indexOf("Read state for 14 miners");
    const responseIndex = transcript.indexOf("All 14 miners are offline.");

    expect(requestIndex).toBeGreaterThanOrEqual(0);
    expect(statusIndex).toBeGreaterThan(requestIndex);
    expect(responseIndex).toBeGreaterThan(statusIndex);
    expect(screen.getByTestId("agent-activity-status")).not.toHaveClass("rounded-2xl");
  });

  test("cancels and ignores an in-flight response when starting a new chat", async () => {
    let releaseStream = () => {};
    let finishStream = () => {};
    const streamReleased = new Promise<void>((resolve) => {
      releaseStream = resolve;
    });
    const streamFinished = new Promise<void>((resolve) => {
      finishStream = resolve;
    });
    let requestSignal: AbortSignal | undefined;
    mocks.sendMessage.mockImplementation((_request, options) => {
      requestSignal = options?.signal;
      return (async function* () {
        try {
          await streamReleased;
          yield { event: { case: "textDelta", value: { content: "Stale response" } } };
        } finally {
          finishStream();
        }
      })();
    });

    render(
      <MemoryRouter>
        <ChatPanel />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Summarize fleet health" }));
    fireEvent.click(await screen.findByRole("button", { name: "New chat" }));

    expect(requestSignal?.aborted).toBe(true);
    await act(async () => {
      releaseStream();
      await streamFinished;
    });

    expect(screen.queryByText("Stale response")).not.toBeInTheDocument();
    expect(useChatStore.getState().messages).toEqual([]);
    expect(useChatStore.getState().agentActivities).toEqual([]);
    expect(useChatStore.getState().isStreaming).toBe(false);
  });

  test("keeps a write paused until the operator confirms it", async () => {
    let releaseConfirmation = () => {};
    const confirmationResolved = new Promise<void>((resolve) => {
      releaseConfirmation = resolve;
    });
    mocks.resolveToolConfirmation.mockImplementation(async () => {
      releaseConfirmation();
      return {};
    });
    mocks.sendMessage.mockImplementation(() =>
      (async function* () {
        yield { event: { case: "toolCall", value: { id: "call-1", summary: "Preparing site creation" } } };
        yield {
          event: {
            case: "confirmationRequired",
            value: {
              confirmationId: "confirmation-1",
              toolCallId: "call-1",
              title: "Create this site?",
              description: "Minerbot will add a new site to your fleet.",
              confirmLabel: "Create site",
              details: [{ label: "Name", value: "North" }],
            },
          },
        };
        await confirmationResolved;
        yield {
          event: {
            case: "toolResult",
            value: { id: "call-1", success: true, cancelled: false, summary: 'Created site "North"' },
          },
        };
        yield { event: { case: "textDelta", value: { content: "North was created." } } };
      })(),
    );

    render(
      <MemoryRouter>
        <ChatPanel />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByRole("textbox", { name: "Message Minerbot" }), {
      target: { value: "Create a site named North" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));

    expect(await screen.findByRole("heading", { name: "Create this site?" })).toBeInTheDocument();
    expect(screen.getByText("North")).toBeInTheDocument();
    expect(screen.queryByText("North was created.")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Create site" }));

    expect(mocks.resolveToolConfirmation).toHaveBeenCalledWith({
      confirmationId: "confirmation-1",
      decision: 1,
    });
    expect(await screen.findByText("North was created.")).toBeInTheDocument();
    expect(screen.getByText("Approved")).toBeInTheDocument();
    expect(screen.getByText('Created site "North"')).toBeInTheDocument();
  });
});
