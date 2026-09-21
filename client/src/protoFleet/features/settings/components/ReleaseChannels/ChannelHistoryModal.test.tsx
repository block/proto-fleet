import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ChannelHistoryModal from "./ChannelHistoryModal";
import { activeRigRollout, canaryChannel, completedWithFailuresRigRollout } from "./ReleaseChannels.fixtures";
import { RolloutDeviceCountsSchema, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const props = () => ({
  channel: canaryChannel,
  rollouts: [],
  acknowledgedRollbacks: [],
  currentRollouts: [],
  onView: vi.fn(),
  onRollback: vi.fn(),
  onClose: vi.fn(),
  onRetry: vi.fn(),
});

describe("channel history progress", () => {
  it.each([
    { name: "all skipped", counts: { skipped: 6 }, expected: "0 updated, 6 skipped" },
    { name: "all excluded", counts: { excluded: 6 }, expected: "0 updated, 6 excluded" },
    {
      name: "mixed outcomes",
      counts: { done: 3, failed: 1, skipped: 1, excluded: 1 },
      expected: "3 of 4 updated, 1 failed, 1 skipped, 1 excluded",
    },
    {
      name: "canceled neutral targets",
      status: RolloutStatus.CANCELED,
      counts: { skipped: 2, excluded: 4 },
      expected: "0 updated, 2 skipped, 4 excluded",
    },
    { name: "empty canceled update", status: RolloutStatus.CANCELED, counts: {}, expected: "—" },
  ])("reports $name distinctly", ({ counts, status = RolloutStatus.COMPLETED, expected }) => {
    const rollout = {
      ...completedWithFailuresRigRollout,
      status,
      deviceCount: Object.values(counts).reduce((sum, count) => sum + count, 0),
      deviceCounts: create(RolloutDeviceCountsSchema, counts),
    };
    render(<ChannelHistoryModal {...props()} rollouts={[rollout]} historyState={{ status: "ready" }} />);
    const cells = within(screen.getByTestId(`history-row-${rollout.id.toString()}`)).getAllByRole("cell");
    expect(cells[3]).toHaveTextContent(expected);
  });
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

describe("acknowledged rollback history", () => {
  it("keeps terminal history but removes rollback and stale active outcomes until reads catch up", () => {
    const source = completedWithFailuresRigRollout;
    const active = { ...activeRigRollout, assignmentGeneration: source.assignmentGeneration, id: source.id + 1n };
    const propsWithHistory = { ...props(), rollouts: [source, active], acknowledgedRollbacks: [source] };
    render(<ChannelHistoryModal {...propsWithHistory} historyState={{ status: "error" }} />);
    const sourceRow = within(screen.getByTestId(`history-row-${source.id}`));
    const activeRow = within(screen.getByTestId(`history-row-${active.id}`));
    expect(sourceRow.getByText("Completed with failures")).toBeInTheDocument();
    expect(activeRow.getByText("Rolled back")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Roll back/ })).not.toBeInTheDocument();
    fireEvent.click(activeRow.getByRole("button", { name: "View" }));
    expect(propsWithHistory.onView).toHaveBeenCalledWith(
      expect.objectContaining({
        id: active.id,
        revision: active.revision,
        status: RolloutStatus.CANCELED,
      }),
    );
  });
});

describe("applied successor history", () => {
  it("invalidates cached actions when the shared cache knows a newer assignment than the channel", () => {
    const source = completedWithFailuresRigRollout;
    const active = { ...activeRigRollout, assignmentGeneration: source.assignmentGeneration };
    const successor = { ...active, id: active.id + 100n, assignmentGeneration: active.assignmentGeneration + 1n };
    const callbacks = props();
    render(
      <ChannelHistoryModal
        {...callbacks}
        rollouts={[source, active]}
        currentRollouts={[successor]}
        historyState={{ status: "error" }}
      />,
    );
    expect(
      within(screen.getByTestId(`history-row-${source.id}`)).getByText("Completed with failures"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Roll back/ })).not.toBeInTheDocument();
    fireEvent.click(within(screen.getByTestId(`history-row-${active.id}`)).getByRole("button", { name: "View" }));
    expect(callbacks.onView).toHaveBeenCalledWith(
      expect.objectContaining({ id: active.id, revision: active.revision, status: RolloutStatus.CANCELED }),
    );
  });
});
