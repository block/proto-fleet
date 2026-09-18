import { fireEvent, render, screen, within } from "@testing-library/react";
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

describe("channel history manufacturer identity", () => {
  it("distinguishes the same model from different manufacturers before opening or rolling back an update", () => {
    const callbacks = props();
    const proto = completedWithFailuresRigRollout;
    const acme = { ...proto, id: proto.id + 1n, manufacturer: "Acme" };
    const rollouts = [proto, acme];
    const channel = {
      ...canaryChannel,
      modelGroups: rollouts.map((rollout) => ({
        ...canaryChannel.modelGroups[0],
        manufacturer: rollout.manufacturer,
        model: rollout.model,
        assignmentGeneration: rollout.assignmentGeneration,
      })),
    };
    render(
      <ChannelHistoryModal {...callbacks} channel={channel} rollouts={rollouts} historyState={{ status: "ready" }} />,
    );

    expect(screen.getByRole("columnheader", { name: "Manufacturer / model" })).toBeInTheDocument();
    const protoRow = within(screen.getByTestId(`history-row-${proto.id.toString()}`));
    const acmeRow = within(screen.getByTestId(`history-row-${acme.id.toString()}`));
    expect(protoRow.getByRole("cell", { name: "Proto Rig" })).toBeInTheDocument();
    expect(acmeRow.getByRole("cell", { name: "Acme Rig" })).toBeInTheDocument();

    fireEvent.click(acmeRow.getByRole("button", { name: "View" }));
    fireEvent.click(protoRow.getByRole("button", { name: "View" }));
    expect(callbacks.onView.mock.calls).toEqual([[acme], [proto]]);

    fireEvent.click(acmeRow.getByRole("button", { name: "Roll back to 1.4.3" }));
    fireEvent.click(protoRow.getByRole("button", { name: "Roll back to 1.4.3" }));
    expect(callbacks.onRollback.mock.calls).toEqual([[acme], [proto]]);
  });
});
