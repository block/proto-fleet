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
      useChatStore.getState().clearMessages();
    });
  });

  afterEach(() => {
    act(() => {
      useChatStore.getState().clearMessages();
    });
  });

  test("renders the cleaned-up chat surface with actionable suggestion cards and history", () => {
    render(
      <MemoryRouter initialEntries={["/minerbot"]}>
        <MinerbotPage />
      </MemoryRouter>,
    );

    const history = screen.getByLabelText("Chat history");
    const historyRows = within(history).getAllByRole("listitem");
    const suggestions = screen.getByLabelText("Actionable suggestions");

    expect(screen.queryByRole("heading", { name: "Minerbot" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Chat" })).not.toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Minerbot" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New chat" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Suggestions" })).toHaveAttribute("aria-current", "page");
    expect(historyRows[0]).toHaveTextContent("Firmware drift review");
    expect(historyRows[0]).not.toHaveTextContent("2 hours ago");
    expect(historyRows[1]).toHaveTextContent("Power strategy");
    expect(historyRows[1]).not.toHaveTextContent("Yesterday");
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

  test("loads previous chats from history", () => {
    render(
      <MemoryRouter initialEntries={["/minerbot"]}>
        <MinerbotPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: /Firmware drift review/ }));

    const conversation = screen.getByLabelText("Conversation");
    const scrollArea = screen.getByTestId("minerbot-chat-scroll-area");

    expect(screen.getByRole("button", { name: /Firmware drift review/ })).toHaveAttribute("aria-current", "page");
    expect(screen.queryByLabelText("Actionable suggestions")).not.toBeInTheDocument();
    expect(scrollArea).toHaveClass("min-h-0", "flex-1", "overflow-y-auto", "scroll-pb-8");
    expect(scrollIntoViewMock).toHaveBeenLastCalledWith({ behavior: "smooth", block: "end", inline: "nearest" });
    expect(conversation).toHaveClass("max-w-[800px]");
    expect(within(conversation).getByRole("heading", { name: "Firmware drift review" })).toBeInTheDocument();
    expect(within(conversation).getByText("2 hours ago")).toBeInTheDocument();
    expect(within(conversation).getByText("Find miners behind firmware and plan a staged update.")).toBeInTheDocument();
    expect(within(conversation).getByText(/I found 8 miners behind/)).toBeInTheDocument();
    expect(
      within(conversation)
        .getByText("Find miners behind firmware and plan a staged update.")
        .closest(".bg-core-primary-fill"),
    ).not.toBeNull();
    expect(
      within(conversation)
        .getByText(/I found 8 miners behind/)
        .closest(".bg-core-primary-5"),
    ).toBeNull();
    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(mocks.sendMessage).not.toHaveBeenCalled();
  });

  test("starts an empty chat from a loaded history thread", () => {
    render(
      <MemoryRouter initialEntries={["/minerbot"]}>
        <MinerbotPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: /Firmware drift review/ }));
    fireEvent.click(screen.getByRole("button", { name: "New chat" }));

    const conversation = screen.getByLabelText("Conversation");
    const promptIdeas = screen.getByLabelText("Suggested prompts");

    expect(screen.getByRole("button", { name: "New chat" })).toHaveAttribute("aria-current", "page");
    expect(screen.queryByLabelText("Actionable suggestions")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "What would you like to know?" })).toBeInTheDocument();
    expect(within(promptIdeas).getAllByRole("button")).toHaveLength(3);
    expect(within(promptIdeas).getByRole("button", { name: "Start a fleet health review" })).toBeInTheDocument();
    expect(
      within(conversation).queryByText("Find miners behind firmware and plan a staged update."),
    ).not.toBeInTheDocument();
    expect(within(conversation).queryByText(/I found 8 miners behind/)).not.toBeInTheDocument();
    expect(mocks.sendMessage).not.toHaveBeenCalled();
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
