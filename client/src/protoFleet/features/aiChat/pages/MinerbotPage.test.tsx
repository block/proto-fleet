import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import MinerbotPage from "./MinerbotPage";
import { useChatStore } from "@/protoFleet/features/aiChat/useChatStore";

const mocks = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  resolveToolConfirmation: vi.fn(),
}));
const scrollIntoViewMock = vi.fn();

vi.mock("@/protoFleet/api/clients", () => ({
  chatClient: mocks,
}));

describe("MinerbotPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    scrollIntoViewMock.mockClear();
    Element.prototype.scrollIntoView = scrollIntoViewMock;
    mocks.sendMessage.mockImplementation(() => (async function* () {})());
    act(() => {
      useChatStore.getState().resetSession();
    });
  });

  afterEach(() => {
    act(() => {
      useChatStore.getState().resetSession();
    });
  });

  test("renders the cleaned-up chat surface with actionable suggestion cards and history", () => {
    render(
      <MemoryRouter initialEntries={["/minerbot"]}>
        <MinerbotPage />
      </MemoryRouter>,
    );

    const history = screen.getByLabelText("Chat history");
    const suggestions = screen.getByLabelText("Actionable suggestions");

    expect(screen.queryByRole("heading", { name: "Minerbot" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Chat" })).not.toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Minerbot" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New chat" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Suggestions" })).toHaveAttribute("aria-current", "page");
    expect(within(history).getByText("No conversations yet")).toBeInTheDocument();
    expect(within(history).queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByText("Firmware drift review")).not.toBeInTheDocument();
    expect(screen.queryByText("Power strategy")).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Suggested workflows" })).not.toBeInTheDocument();
    expect(within(suggestions).getAllByTestId("minerbot-suggestion-card")).toHaveLength(6);
    expect(screen.getByRole("heading", { name: "Forecast failing hardware" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Forecast failures" })).toBeInTheDocument();
  });

  test("sends actionable suggestions through the shared Minerbot conversation path", async () => {
    render(
      <MemoryRouter initialEntries={["/minerbot"]}>
        <MinerbotPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Forecast failures" }));

    await waitFor(() => {
      expect(mocks.sendMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          content: "Forecast failing hardware and recommend the highest-priority repairs.",
        }),
        expect.any(Object),
      );
    });
    expect(
      within(screen.getByLabelText("Conversation")).getByText(
        "Forecast failing hardware and recommend the highest-priority repairs.",
      ),
    ).toBeInTheDocument();
  });

  test("loads actual previous chats and sends their real transcript as history", async () => {
    mocks.sendMessage.mockImplementation(() =>
      (async function* () {
        yield { event: { case: "textDelta", value: { content: "Observed 3 offline miners." } } };
      })(),
    );
    render(
      <MemoryRouter initialEntries={["/minerbot"]}>
        <MinerbotPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Forecast failures" }));
    await screen.findByText("Observed 3 offline miners.");
    const originalId = mocks.sendMessage.mock.calls[0][0].conversationId;
    fireEvent.click(screen.getByRole("button", { name: "New chat" }));
    const history = screen.getByLabelText("Chat history");
    fireEvent.click(
      within(history).getByRole("button", {
        name: "Forecast failing hardware and recommend the highest-priority repairs.",
      }),
    );

    const conversation = screen.getByLabelText("Conversation");
    expect(within(conversation).getByText("Observed 3 offline miners.")).toBeInTheDocument();
    expect(screen.getByTestId("minerbot-chat-scroll-area")).toHaveClass(
      "min-h-0",
      "flex-1",
      "overflow-y-auto",
      "scroll-pb-8",
    );
    expect(scrollIntoViewMock).toHaveBeenLastCalledWith({ behavior: "smooth", block: "end", inline: "nearest" });
    expect(conversation).toHaveClass("max-w-[800px]");
    expect(within(conversation).queryByText(/I found 8 miners behind/)).not.toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox", { name: "Message Minerbot" }), { target: { value: "What changed?" } });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    await waitFor(() => expect(mocks.sendMessage).toHaveBeenCalledTimes(2));
    expect(mocks.sendMessage.mock.calls[1][0]).toMatchObject({
      conversationId: originalId,
      history: [
        { role: 1, content: "Forecast failing hardware and recommend the highest-priority repairs." },
        { role: 2, content: "Observed 3 offline miners." },
      ],
    });
  });

  test("starts an empty chat while keeping the actual prior conversation in history", async () => {
    mocks.sendMessage.mockImplementation(() =>
      (async function* () {
        yield { event: { case: "textDelta", value: { content: "Actual fleet reply" } } };
      })(),
    );
    render(
      <MemoryRouter initialEntries={["/minerbot"]}>
        <MinerbotPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Plan updates" }));
    await screen.findByText("Actual fleet reply");
    fireEvent.click(screen.getByRole("button", { name: "New chat" }));

    expect(screen.getByRole("button", { name: "New chat" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("heading", { name: "What would you like to know?" })).toBeInTheDocument();
    expect(within(screen.getByLabelText("Suggested prompts")).getAllByRole("button")).toHaveLength(3);
    expect(
      within(screen.getByLabelText("Chat history")).getByRole("button", {
        name: "Find miners behind firmware and plan a staged update.",
      }),
    ).toBeInTheDocument();
    expect(within(screen.getByLabelText("Conversation")).queryByText("Actual fleet reply")).not.toBeInTheDocument();
    expect(useChatStore.getState().messages).toEqual([]);
  });

  test("starts a new chat from a prompt idea", async () => {
    render(
      <MemoryRouter initialEntries={["/minerbot"]}>
        <MinerbotPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "New chat" }));
    fireEvent.click(screen.getByRole("button", { name: "Plan recurring work" }));

    await waitFor(() => {
      expect(mocks.sendMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          content: "Plan recurring work",
        }),
        expect.any(Object),
      );
    });
    expect(within(screen.getByLabelText("Conversation")).getByText("Plan recurring work")).toBeInTheDocument();
  });
});
