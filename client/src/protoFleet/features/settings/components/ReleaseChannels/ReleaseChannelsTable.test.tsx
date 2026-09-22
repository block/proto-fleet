import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import {
  activeRigRollout,
  canaryChannel,
  completedRigRollout,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import ReleaseChannelsTable from "./ReleaseChannelsTable";
import {
  ReleaseChannelModelGroupSchema,
  RolloutDeviceCountsSchema,
  RolloutSchema,
  RolloutState,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

afterEach(() => vi.restoreAllMocks());

describe("release channel firmware availability", () => {
  it.each([false, true])("shows unavailable assignments with an active update: %s", (active) => {
    const group = {
      ...canaryChannel.modelGroups[0],
      firmwareAvailable: false,
      firmwareFileId: "",
      activeRolloutId: active ? activeRigRollout.id : 0n,
      assignmentGeneration: activeRigRollout.assignmentGeneration,
      onTargetCount: canaryChannel.modelGroups[0].minerCount,
    };
    const channel = { ...canaryChannel, modelGroups: [group, canaryChannel.modelGroups[1]] };
    const props = { channels: [channel], rollouts: [activeRigRollout], onCreate: vi.fn(), onManage: vi.fn() };
    const { rerender } = render(<ReleaseChannelsTable {...props} />);
    expect(screen.getByTestId("channel-status-Canary").textContent).toBe(
      active ? "1 firmware assignment unavailable; 1 updating" : "1 firmware assignment unavailable",
    );
    fireEvent.click(screen.getByRole("button", { name: "Expand Canary models" }));
    const row = screen.getByTestId("model-row-Canary-Rig").closest("tr")!;
    expect(within(row).getAllByRole("cell")[2]).toHaveTextContent(group.firmwareVersion);
    expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent(/^Assigned firmware unavailable$/);
    expect(screen.getByTestId(`model-status-Canary-${canaryChannel.modelGroups[1].model}`)).toHaveTextContent(
      /^No firmware assigned$/,
    );

    // The same payload may become available under a different upload ID.
    rerender(
      <ReleaseChannelsTable
        {...props}
        channels={[
          {
            ...channel,
            modelGroups: [{ ...group, firmwareAvailable: true, firmwareFileId: "restored-upload" }],
          },
        ]}
      />,
    );
    expect(screen.getByTestId("channel-status-Canary").textContent).toBe(active ? "1 updating" : "No active updates");
    expect(screen.getByTestId("model-status-Canary-Rig").textContent).toBe(active ? "Updating, 2 of 6" : "Up to date");
  });

  it("counts unavailable canonical assignments once even when another rollout summary is still loading", () => {
    const unavailable = {
      ...canaryChannel.modelGroups[0],
      firmwareAvailable: false,
      firmwareFileId: "",
      activeRolloutId: 0n,
    };
    render(
      <ReleaseChannelsTable
        channels={[
          {
            ...canaryChannel,
            modelGroups: [
              { ...canaryChannel.modelGroups[0], model: "Waiting", activeRolloutId: 99n },
              unavailable,
              { ...unavailable, manufacturer: " proto ", model: " rig " },
              { ...unavailable, model: "Other", firmwareTargetModel: "Other" },
            ],
          },
        ]}
        rollouts={[]}
        onCreate={vi.fn()}
        onManage={vi.fn()}
      />,
    );
    expect(screen.getByTestId("channel-status-Canary").textContent).toBe(
      "2 firmware assignments unavailable; Refreshing update status",
    );
  });
});

describe("release channel current assignment history", () => {
  it.each([
    { finished: completedRigRollout, onTargetCount: 6, label: "Up to date" },
    { finished: completedWithFailuresRigRollout, onTargetCount: 2, label: "2 of 6 on target" },
  ])(
    "ignores old-generation completion after the current update is canceled: $label",
    ({ finished, onTargetCount, label }) => {
      const group = create(ReleaseChannelModelGroupSchema, {
        ...canaryChannel.modelGroups[0],
        assignmentGeneration: 2n,
        activeRolloutId: 0n,
        onTargetCount,
      });
      const previous = create(RolloutSchema, {
        ...finished,
        assignmentGeneration: 1n,
        firmwareVersion: group.firmwareVersion,
        firmwareChecksum: group.firmwareChecksum,
      });
      const canceled = create(RolloutSchema, {
        ...previous,
        id: 99n,
        assignmentGeneration: 2n,
        status: RolloutStatus.CANCELED,
        state: RolloutState.CANCELED,
      });
      render(
        <ReleaseChannelsTable
          channels={[{ ...canaryChannel, modelGroups: [group] }]}
          rollouts={[canceled, previous]}
          onCreate={vi.fn()}
          onManage={vi.fn()}
        />,
      );
      fireEvent.click(screen.getByRole("button", { name: "Expand Canary models" }));
      expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent(new RegExp(`^${label}$`));
    },
  );

  it("selects the latest completion within the displayed generation rather than another generation's newer history", () => {
    const group = create(ReleaseChannelModelGroupSchema, {
      ...canaryChannel.modelGroups[0],
      assignmentGeneration: 2n,
      activeRolloutId: 0n,
      onTargetCount: 2,
    });
    const finished = create(RolloutSchema, {
      ...completedWithFailuresRigRollout,
      assignmentGeneration: 2n,
      finishedAt: create(TimestampSchema, { seconds: 20n }),
      deviceCounts: create(RolloutDeviceCountsSchema, { done: 4, failed: 2 }),
    });
    const earlier = create(RolloutSchema, {
      ...finished,
      id: 49n,
      finishedAt: create(TimestampSchema, { seconds: 10n }),
      deviceCounts: create(RolloutDeviceCountsSchema, { done: 5, failed: 1 }),
    });
    const otherGeneration = create(RolloutSchema, {
      ...completedRigRollout,
      assignmentGeneration: 3n,
      finishedAt: create(TimestampSchema, { seconds: 30n }),
    });
    const otherChannel = create(RolloutSchema, {
      ...finished,
      channelId: 2n,
      finishedAt: create(TimestampSchema, { seconds: 40n }),
      deviceCounts: create(RolloutDeviceCountsSchema, { done: 1, failed: 5 }),
    });
    render(
      <ReleaseChannelsTable
        channels={[{ ...canaryChannel, modelGroups: [group] }]}
        rollouts={[otherGeneration, earlier, otherChannel, finished]}
        onCreate={vi.fn()}
        onManage={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Expand Canary models" }));
    expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent(/^2 failed to update$/);
  });
});

describe("release channel update summary", () => {
  it.each([2n, 3n])(
    "waits for the reported rollout instead of displaying an earlier scan (generation %s)",
    (generation) => {
      const current = { ...activeRigRollout, id: 99n, assignmentGeneration: generation };
      const group = { ...canaryChannel.modelGroups[0], activeRolloutId: current.id, assignmentGeneration: generation };
      const channel = { ...canaryChannel, modelGroups: [group] };
      const props = { channels: [channel], rollouts: [gatedRigRollout], onCreate: vi.fn(), onManage: vi.fn() };
      const { rerender } = render(<ReleaseChannelsTable {...props} />);
      fireEvent.click(screen.getByRole("button", { name: "Expand Canary models" }));
      expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent(/^Refreshing update status$/);
      expect(screen.getByTestId("channel-status-Canary")).toHaveTextContent(/^Refreshing update status$/);
      rerender(<ReleaseChannelsTable {...props} rollouts={[current, gatedRigRollout]} />);
      expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent(/^Updating, 2 of 6$/);
      expect(screen.getByTestId("channel-status-Canary")).toHaveTextContent(/^1 updating$/);
      // A later group read can also report completion before the rollout scan does.
      rerender(
        <ReleaseChannelsTable
          {...props}
          channels={[{ ...channel, modelGroups: [{ ...group, activeRolloutId: 0n }] }]}
          rollouts={[current, gatedRigRollout]}
        />,
      );
      expect(screen.getByTestId("channel-status-Canary")).toHaveTextContent(/^No active updates$/);
      expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent(/^2 of 6 on target$/);
    },
  );

  const rawRigGroups = [
    { manufacturer: "Proto", model: "Rig" },
    { manufacturer: "proto", model: "rig" },
    { manufacturer: " Proto ", model: " Rig " },
  ].map((pair) => create(ReleaseChannelModelGroupSchema, { ...pair, minerCount: 1 }));

  it.each([
    { rollout: activeRigRollout, label: "1 updating" },
    { rollout: pausedRigRollout, label: "1 updating, 1 paused" },
    { rollout: gatedRigRollout, label: "1 updating, 1 needs attention" },
  ])("counts raw model variants as one update: $label", ({ rollout, label }) => {
    render(
      <ReleaseChannelsTable
        channels={[
          {
            ...canaryChannel,
            modelGroups: rawRigGroups.map((group) => ({
              ...group,
              activeRolloutId: rollout.id,
              assignmentGeneration: rollout.assignmentGeneration,
            })),
          },
        ]}
        rollouts={[rollout]}
        onCreate={vi.fn()}
        onManage={vi.fn()}
      />,
    );

    expect(screen.getByTestId("channel-status-Canary").textContent).toBe(label);
  });

  it("counts distinct updates independently when each has raw model variants", () => {
    const pairs = [
      { manufacturer: "Proto", model: "Rig" },
      { manufacturer: "Proto", model: "Rig 2" },
      { manufacturer: "Bitmain", model: "S21" },
    ];
    const updates = [activeRigRollout, pausedRigRollout, gatedRigRollout].map((rollout, index) => ({
      ...rollout,
      ...pairs[index],
    }));
    const modelGroups = updates.flatMap((rollout) => [
      create(ReleaseChannelModelGroupSchema, {
        manufacturer: rollout.manufacturer,
        model: rollout.model,
        assignmentGeneration: rollout.assignmentGeneration,
        activeRolloutId: rollout.id,
        minerCount: 1,
      }),
      create(ReleaseChannelModelGroupSchema, {
        manufacturer: ` ${rollout.manufacturer.toLowerCase()} `,
        model: ` ${rollout.model.toLowerCase()} `,
        activeRolloutId: rollout.id,
        assignmentGeneration: rollout.assignmentGeneration,
        minerCount: 1,
      }),
    ]);
    render(
      <ReleaseChannelsTable
        channels={[{ ...canaryChannel, modelGroups }]}
        rollouts={[...updates, { ...activeRigRollout, id: 100n, channelId: 2n }]}
        onCreate={vi.fn()}
        onManage={vi.fn()}
      />,
    );

    expect(screen.getByTestId("channel-status-Canary").textContent).toBe("3 updating, 1 needs attention, 1 paused");
  });
});

describe("release channel model row identity", () => {
  it("isolates BOM models from active ASCII updates while joining Go whitespace aliases", () => {
    const modelGroups = ["Rig", "\uFEFFRig", "Rig\u0085"].map((model) =>
      create(ReleaseChannelModelGroupSchema, {
        manufacturer: "Proto",
        model,
        minerCount: 1,
        activeRolloutId: model === "\uFEFFRig" ? 0n : activeRigRollout.id,
        assignmentGeneration: activeRigRollout.assignmentGeneration,
      }),
    );
    render(
      <ReleaseChannelsTable
        channels={[{ ...canaryChannel, modelGroups }]}
        rollouts={[activeRigRollout]}
        onCreate={vi.fn()}
        onManage={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Expand Canary models" }));
    expect(screen.getByTestId("model-status-Canary-\uFEFFRig", { normalizer: (text) => text })).toHaveTextContent(
      /^No firmware assigned$/,
    );
    const canonicalStatus = screen.getByTestId("model-status-Canary-Rig").textContent;
    expect(canonicalStatus).not.toBe("No firmware assigned");
    expect(screen.getByTestId("model-status-Canary-Rig\u0085", { normalizer: (text) => text }).textContent).toBe(
      canonicalStatus,
    );
  });

  it.each([
    {
      name: "colon-containing identities",
      first: { manufacturer: "a:b", model: "c" },
      second: { manufacturer: "a", model: "b:c" },
    },
    {
      name: "distinct raw case variants",
      first: { manufacturer: "PROTO", model: "Rig" },
      second: { manufacturer: "proto", model: "rig" },
    },
  ])("preserves the right rows when $name reorder, update, and disappear", ({ first, second }) => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const firstGroup = create(ReleaseChannelModelGroupSchema, { ...first, minerCount: 2 });
    const secondGroup = create(ReleaseChannelModelGroupSchema, { ...second, minerCount: 3 });
    const channel = { ...canaryChannel, minerCount: 5, modelGroups: [firstGroup, secondGroup] };
    const props = { channels: [channel], rollouts: [], onCreate: vi.fn(), onManage: vi.fn() };
    const { rerender } = render(<ReleaseChannelsTable {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Canary models" }));
    const firstRow = screen.getByTestId(`model-row-Canary-${first.model}`).closest("tr")!;
    const secondRow = screen.getByTestId(`model-row-Canary-${second.model}`).closest("tr")!;
    expect(within(firstRow).getAllByRole("cell")[1]).toHaveTextContent(/^2$/);
    expect(within(secondRow).getAllByRole("cell")[1]).toHaveTextContent(/^3$/);

    rerender(
      <ReleaseChannelsTable
        {...props}
        channels={[{ ...channel, minerCount: 6, modelGroups: [{ ...secondGroup, minerCount: 4 }, firstGroup] }]}
      />,
    );
    expect(screen.getByTestId(`model-row-Canary-${first.model}`).closest("tr")).toBe(firstRow);
    expect(screen.getByTestId(`model-row-Canary-${second.model}`).closest("tr")).toBe(secondRow);
    expect(within(secondRow).getAllByRole("cell")[1]).toHaveTextContent(/^4$/);

    rerender(
      <ReleaseChannelsTable
        {...props}
        channels={[{ ...channel, minerCount: 4, modelGroups: [{ ...secondGroup, minerCount: 4 }] }]}
      />,
    );
    expect(screen.queryByTestId(`model-row-Canary-${first.model}`)).not.toBeInTheDocument();
    expect(firstRow).not.toBeInTheDocument();
    expect(screen.getByTestId(`model-row-Canary-${second.model}`).closest("tr")).toBe(secondRow);
    expect(screen.getAllByTestId("list-row")).toHaveLength(2);
    expect(errors).not.toHaveBeenCalled();
  });
});

describe("acknowledged rollback assignment", () => {
  it("shows pending assignment details instead of the superseded target and rollout", () => {
    const group = {
      ...canaryChannel.modelGroups[0],
      rollbackPending: true,
      activeRolloutId: activeRigRollout.id,
      assignmentGeneration: activeRigRollout.assignmentGeneration,
      firmwareAvailable: false,
    };
    render(
      <ReleaseChannelsTable
        channels={[{ ...canaryChannel, modelGroups: [group] }]}
        rollouts={[activeRigRollout]}
        onCreate={vi.fn()}
        onManage={vi.fn()}
      />,
    );
    expect(screen.getByTestId("channel-status-Canary")).toHaveTextContent("Rollback saved; refreshing assignment");
    fireEvent.click(screen.getByRole("button", { name: "Expand Canary models" }));
    const row = within(screen.getByTestId("model-row-Canary-Rig").closest("tr")!);
    expect(row.getByText("Refreshing assignment")).toBeInTheDocument();
    expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent("Rollback saved; refreshing assignment");
    expect(row.queryByText("Assigned firmware unavailable")).not.toBeInTheDocument();
    expect(row.queryByText(group.firmwareVersion)).not.toBeInTheDocument();
  });
});
