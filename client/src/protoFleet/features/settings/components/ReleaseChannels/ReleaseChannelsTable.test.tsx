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
        channels={[{ ...canaryChannel, modelGroups: rawRigGroups }]}
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
    const modelGroups = pairs.flatMap((pair) => [
      create(ReleaseChannelModelGroupSchema, { ...pair, minerCount: 1 }),
      create(ReleaseChannelModelGroupSchema, {
        manufacturer: ` ${pair.manufacturer.toLowerCase()} `,
        model: ` ${pair.model.toLowerCase()} `,
        minerCount: 1,
      }),
    ]);
    render(
      <ReleaseChannelsTable
        channels={[{ ...canaryChannel, modelGroups }]}
        rollouts={[
          { ...activeRigRollout, ...pairs[0] },
          { ...pausedRigRollout, ...pairs[1] },
          { ...gatedRigRollout, ...pairs[2] },
          { ...activeRigRollout, id: 100n, channelId: 2n },
        ]}
        onCreate={vi.fn()}
        onManage={vi.fn()}
      />,
    );

    expect(screen.getByTestId("channel-status-Canary").textContent).toBe("3 updating, 1 needs attention, 1 paused");
  });
});

describe("release channel model row identity", () => {
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
