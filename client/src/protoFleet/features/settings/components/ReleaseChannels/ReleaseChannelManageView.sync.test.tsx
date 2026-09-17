import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ReleaseChannelManageView from "./ReleaseChannelManageView";
import { canaryChannel } from "./ReleaseChannels.fixtures";
import {
  type PreviewReleaseChannelScopeResponse,
  PreviewReleaseChannelScopeResponseSchema,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ChannelView, ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";

vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useHasPermission: () => true,
}));
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: ({ open, onSave }: { open: boolean; onSave: (selection: { siteIds: string[] }) => void }) =>
    open ? <button onClick={() => onSave({ siteIds: ["9"] })}>Choose site 9</button> : null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

const channelFor = (): ChannelView => ({
  ...canaryChannel,
  name: "Original name",
  description: "Original description",
  scope: { ...canaryChannel.scope!, rackIds: [], siteIds: [1n], deviceIdentifiers: [] },
  behavior: create(RolloutBehaviorSchema, {
    method: RolloutMethod.BATCHED,
    order: RolloutOrder.RANDOM,
    batchSize: 10,
    maxConcurrentOffline: 4,
  }),
});

function renderManage(
  channel = channelFor(),
  onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockResolvedValue(),
  previewScope = vi.fn().mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema)),
) {
  const props = {
    channel,
    rollouts: [],
    firmwareFiles: [],
    minerNames: {},
    previewScope,
    listChannelMiners: vi.fn().mockResolvedValue([]),
    listRolloutDevices: vi.fn().mockResolvedValue([]),
    onSave,
    onApply: vi.fn().mockResolvedValue(undefined),
  };
  const { rerender } = render(<ReleaseChannelManageView {...props} />);
  return {
    ...props,
    update: (next: ChannelView, hasRefreshError = false) =>
      rerender(<ReleaseChannelManageView {...props} channel={next} hasRefreshError={hasRefreshError} />),
  };
}

function chooseMethod(label: string) {
  fireEvent.click(screen.getByTestId("rollout-method"));
  fireEvent.click(screen.getByRole("option", { name: new RegExp(`^${label}`) }));
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("release channel settings refresh", () => {
  it("blocks saving an oversized scope without blocking staged firmware and recovers after correction", () => {
    const channel = channelFor();
    channel.scope = { ...channel.scope!, siteIds: Array.from({ length: 101 }, (_, index) => BigInt(index + 1)) };
    const { onSave } = renderManage(channel);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Local name" } });
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));

    expect(screen.getByRole("alert")).toHaveTextContent("Select no more than 100 sites (101 selected).");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 9" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    expect(screen.getByLabelText("Name")).toHaveValue("Local name");
  });

  it("keeps a pending scope preview through equivalent polling snapshots", async () => {
    vi.useFakeTimers();
    let finish!: (response: PreviewReleaseChannelScopeResponse) => void;
    const previewScope = vi.fn().mockReturnValue(
      new Promise<PreviewReleaseChannelScopeResponse>((resolve) => {
        finish = resolve;
      }),
    );
    const { channel, update } = renderManage(channelFor(), undefined, previewScope);
    await act(async () => vi.advanceTimersByTime(300));
    expect(previewScope).toHaveBeenCalledOnce();
    for (let poll = 0; poll < 3; poll += 1) {
      update({ ...channel, scope: { ...channel.scope!, siteIds: [1n] } });
      await act(async () => vi.advanceTimersByTime(5000));
    }
    expect(previewScope).toHaveBeenCalledOnce();
    await act(async () => finish(create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 17, modelCount: 1 })));
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("17 miners");
  });

  it("updates clean settings without losing staged firmware when the channel and assignment change", async () => {
    const { channel, update, onSave } = renderManage();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    const next = {
      ...channel,
      name: "Remote name",
      description: "Remote description",
      scope: { ...channel.scope!, siteIds: [2n] },
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.PILOT_THEN_CONTINUE,
        order: RolloutOrder.LEAST_EFFICIENT_FIRST,
        pilotSize: 3,
        maxConcurrentOffline: 6,
      }),
      modelGroups: channel.modelGroups.map((group) => ({
        ...group,
        assignmentGeneration: group.assignmentGeneration + 1n,
      })),
    };
    update(next);

    expect(screen.getByLabelText("Name")).toHaveValue(next.name);
    expect(screen.getByLabelText("Description")).toHaveValue(next.description);
    expect(screen.getByTestId("rollout-method")).toHaveTextContent("Pilot batch, then remaining");
    expect(screen.getByLabelText("Pilot batch size (miners)")).toHaveValue("3");
    expect(screen.getByLabelText("Max miners offline at once (0 for no limit)")).toHaveValue("6");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Local rename" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        name: "Local rename",
        description: next.description,
        scope: next.scope,
        behavior: next.behavior,
      }),
    );
  });

  it("rebases untouched fields while keeping local name, scope, and invalid numeric edits", async () => {
    const { channel, update, onSave } = renderManage();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Local name" } });
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 9" }));
    fireEvent.change(screen.getByLabelText("Max miners offline at once (0 for no limit)"), {
      target: { value: "oops" },
    });
    update({
      ...channel,
      name: "Remote name",
      description: "Remote description",
      scope: { ...channel.scope!, siteIds: [2n] },
      behavior: create(RolloutBehaviorSchema, { ...channel.behavior!, batchSize: 20, maxConcurrentOffline: 8 }),
    });

    expect(screen.getByLabelText("Name")).toHaveValue("Local name");
    expect(screen.getByLabelText("Description")).toHaveValue("Remote description");
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue("20");
    expect(screen.getByLabelText("Max miners offline at once (0 for no limit)")).toHaveValue("oops");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Max miners offline at once (0 for no limit)"), { target: { value: "7" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        name: "Local name",
        description: "Remote description",
        scope: expect.objectContaining({ siteIds: [9n] }),
        behavior: expect.objectContaining({ batchSize: 20, maxConcurrentOffline: 7 }),
      }),
    );
  });

  it("replaces old numeric display text when the authoritative numeric value changes", () => {
    const { channel, update } = renderManage();
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "010" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    update({ ...channel, behavior: create(RolloutBehaviorSchema, { ...channel.behavior!, batchSize: 15 }) });
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue("15");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  it("preserves hidden invalid numeric text after saving another method and refreshing", async () => {
    const { channel, update } = renderManage();
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "unfinished" } });
    chooseMethod("Single batch");
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    const next = {
      ...channel,
      name: "Remote name",
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.ALL_AT_ONCE,
        order: RolloutOrder.RANDOM,
        maxConcurrentOffline: 4,
      }),
    };
    update(next);
    expect(screen.getByLabelText("Name")).toHaveValue("Remote name");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    chooseMethod("Multiple batches");
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue("unfinished");
    expect(screen.getByLabelText("Batch size (miners)")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  it("preserves a newer edit that reverts to the old name while a successful save is pending", async () => {
    let finish!: () => void;
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );
    const { channel, update } = renderManage(channelFor(), onSave);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Submitted name" } });
    fireEvent.click(screen.getByTestId("save-channel"));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: channel.name } });
    update({ ...channel, name: "Submitted name", description: "Remote description" });
    expect(screen.getByLabelText("Description")).toHaveValue(channel.description);
    await act(async () => finish());

    expect(screen.getByLabelText("Name")).toHaveValue(channel.name);
    expect(screen.getByLabelText("Description")).toHaveValue("Remote description");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Submitted name" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  it("uses a successful save acknowledgement until reads recover, then follows new remote settings", async () => {
    const { channel, update, onSave } = renderManage();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved name" } });
    update(channel, true);
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(screen.getByLabelText("Name")).toHaveValue("Saved name");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    update({ ...channel }, true);
    expect(screen.getByLabelText("Name")).toHaveValue("Saved name");
    update({ ...channel, name: "Remote after save" });
    expect(screen.getByLabelText("Name")).toHaveValue("Remote after save");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(onSave).toHaveBeenCalledOnce();
  });
});
