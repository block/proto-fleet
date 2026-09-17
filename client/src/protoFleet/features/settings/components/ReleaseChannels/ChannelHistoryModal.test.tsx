import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import ChannelHistoryModal from "./ChannelHistoryModal";
import { canaryChannel, completedWithFailuresRigRollout } from "./ReleaseChannels.fixtures";

const props = () => ({
  channel: canaryChannel,
  rollouts: [],
  onView: vi.fn(),
  onRollback: vi.fn(),
  onClose: vi.fn(),
  onRetry: vi.fn(),
});

describe("channel history loading", () => {
  it("shows loading and retry instead of claiming incomplete history is empty", () => {
    const callbacks = props();
    const { rerender } = render(<ChannelHistoryModal {...callbacks} historyState={{ status: "loading" }} />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading update history");
    expect(screen.queryByText("No updates for this channel yet.")).not.toBeInTheDocument();
    rerender(<ChannelHistoryModal {...callbacks} historyState={{ status: "error", error: "History timed out" }} />);
    expect(screen.getByRole("alert")).toHaveTextContent("History timed out");
    expect(screen.queryByText("No updates for this channel yet.")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry update history" }));
    expect(callbacks.onRetry).toHaveBeenCalledOnce();
    rerender(<ChannelHistoryModal {...callbacks} historyState={{ status: "ready" }} />);
    expect(screen.getByText("No updates for this channel yet.")).toBeInTheDocument();
  });

  it("keeps observed updates viewable when the full history fails", () => {
    const callbacks = props();
    const rollout = completedWithFailuresRigRollout;
    render(<ChannelHistoryModal {...callbacks} rollouts={[rollout]} historyState={{ status: "error" }} />);
    expect(screen.getByRole("alert")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId(`history-view-${rollout.id.toString()}`));
    expect(callbacks.onView).toHaveBeenCalledExactlyOnceWith(rollout);
  });
});
