import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  applyChannelSettings,
  closeChannelSettings,
  deferred,
  manageViewProps,
  openChannelSettings,
} from "./__tests__/helpers";
import { defaultBehavior } from "./behaviorUtils";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import { activeRigRollout, canaryChannel, gatedRigRollout } from "./ReleaseChannels.fixtures";
import {
  type PreviewReleaseChannelScopeResponse,
  PreviewReleaseChannelScopeResponseSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
  ReleaseChannelScopeSchema,
  RolloutAutomationThresholdsSchema,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { ChannelView, ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";
import { useHasPermission } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useHasPermission: vi.fn(() => true),
}));

// Keep the management view and debounced scope preview real; the shared
// selection modals' fleet-data loading is outside this save-flow regression.
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: ({ open, onSave }: { open: boolean; onSave: (selection: { siteIds: string[] }) => void }) =>
    open ? (
      <>
        <button onClick={() => onSave({ siteIds: ["2"] })}>Choose site 2</button>
        <button onClick={() => onSave({ siteIds: [] })}>Clear sites</button>
        <button onClick={() => onSave({ siteIds: ["2", "1"] })}>Choose sites 2 and 1</button>
      </>
    ) : null,
  BuildingSelectionModal: ({ onSave }: { onSave: (ids: string[]) => void }) => (
    <button onClick={() => onSave(["2"])}>Choose building 2</button>
  ),
  RackSelectionModal: ({ onSave }: { onSave: (ids: string[]) => void }) => (
    <button onClick={() => onSave(["2"])}>Choose rack 2</button>
  ),
  GroupSelectionModal: ({ onSave }: { onSave: (ids: string[]) => void }) => (
    <button onClick={() => onSave(["2"])}>Choose group 2</button>
  ),
  MinerSelectionModal: ({ onSave }: { onSave: (selection: { selectedMinerIds: string[] }) => void }) => (
    <button onClick={() => onSave({ selectedMinerIds: ["miner-2"] })}>Choose miner 2</button>
  ),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

const existingChannel = (): ChannelView => ({
  ...create(ReleaseChannelSchema, {
    id: 1n,
    name: "Production",
    scope: { siteIds: [1n] },
    behavior: defaultBehavior(),
  }),
  modelGroups: [],
});

function renderManage(
  channel: ChannelView | undefined,
  onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockResolvedValue(undefined),
  hasRefreshError = false,
  firmwareFiles: FirmwareFileInfo[] = [],
) {
  const previewScope = vi.fn().mockResolvedValue(
    create(PreviewReleaseChannelScopeResponseSchema, {
      minerCount: 1,
      conflicts: [{ channelId: 2n, channelName: "Canary", minerCount: 1 }],
      conflictCount: 1,
    }),
  );
  const props = {
    ...manageViewProps(),
    channel,
    hasRefreshError,
    firmwareFiles,
    previewScope,
    onSave,
  };
  const { rerender } = render(<ReleaseChannelManageView {...props} />);
  return {
    onSave,
    onApply: props.onApply,
    listChannelMiners: props.listChannelMiners,
    previewScope,
    updateChannel: (nextChannel: ChannelView, refreshError: boolean) =>
      rerender(<ReleaseChannelManageView {...props} channel={nextChannel} hasRefreshError={refreshError} />),
    updateFirmwareFiles: (files: FirmwareFileInfo[]) =>
      rerender(<ReleaseChannelManageView {...props} firmwareFiles={files} />),
  };
}

function renderSettings(...args: Parameters<typeof renderManage>) {
  const result = renderManage(...args);
  if (args[0]) openChannelSettings();
  return result;
}

const assignedChannel = (firmwareFileId = ""): ChannelView => ({
  ...existingChannel(),
  modelGroups: [
    create(ReleaseChannelModelGroupSchema, {
      manufacturer: "proto",
      model: "Rig",
      minerCount: 1,
      firmwareFileId,
      firmwareChecksum: "a".repeat(64),
      firmwareVersion: "1.4.3",
      firmwareTargetManufacturer: "Proto",
      firmwareTargetModel: "Rig",
      firmwareAvailable: firmwareFileId !== "",
      assignmentGeneration: 1n,
    }),
  ],
});

const replacementFile: FirmwareFileInfo = {
  id: "replacement",
  filename: "rig-1.4.4.swu",
  size: 100,
  uploaded_at: "2026-09-17T00:00:00Z",
  target_manufacturer: "Proto",
  target_model: "Rig",
  firmware_version: "1.4.4",
};

const deferredWrite = deferred<void>;

describe("release channel active sizing validation", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });

  afterEach(() => vi.restoreAllMocks());

  test("shows saved telemetry coverage, blocks invalid edits, and saves a cleared server default", async () => {
    const channel = {
      ...existingChannel(),
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.BATCHED,
        batchSize: 1,
        reviewAfterEachBatch: true,
        autoContinueOnHealthyTelemetry: true,
        thresholds: { maxHashrateDropPercent: 10, minSampleCoveragePercent: 50 },
      }),
    };
    const { onSave } = renderSettings(channel);
    const coverage = screen.getByLabelText("Min sample coverage (%)");
    expect(coverage).toHaveValue("50");
    fireEvent.change(coverage, { target: { value: "0" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(coverage, { target: { value: "75" } });
    await applyChannelSettings();
    expect(onSave.mock.calls[0][0].behavior.thresholds).toEqual(
      expect.objectContaining({ maxHashrateDropPercent: 10, minSampleCoveragePercent: 75 }),
    );
    fireEvent.change(screen.getByLabelText("Min sample coverage (%)"), { target: { value: "" } });
    await applyChannelSettings();
    expect(onSave.mock.calls[1][0].behavior.thresholds?.minSampleCoveragePercent).toBeUndefined();
    expect(onSave.mock.calls[1][0].behavior.thresholds?.maxHashrateDropPercent).toBe(10);
  });

  test.each([
    ["Multiple batches", "Batch size (miners)", "batchSize", ""],
    ["Multiple batches", "Batch size (miners)", "batchSize", "0"],
    ["Multiple batches", "Batch size (miners)", "batchSize", "-1"],
    ["Pilot batch, then remaining", "Pilot batch size (miners)", "pilotSize", ""],
    ["Pilot batch, then remaining", "Pilot batch size (miners)", "pilotSize", "0"],
    ["Pilot batch, then remaining", "Pilot batch size (miners)", "pilotSize", "-1"],
  ] as const)(
    "blocks %s with active size %s/%s=%s, then recovers or switches methods",
    async (method, label, field, invalidValue) => {
      const { onSave } = renderSettings(existingChannel());
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed channel" } });
      const chooseMethod = (name: string) => {
        fireEvent.click(screen.getByTestId("rollout-method"));
        fireEvent.click(screen.getByRole("option", { name: new RegExp(`^${name}`) }));
      };
      chooseMethod(method);
      fireEvent.change(screen.getByLabelText(label), { target: { value: invalidValue } });
      expect(screen.getByLabelText(label)).toHaveAttribute("aria-invalid", "true");
      await waitFor(() => expect(screen.getByText("Enter at least 1 miner.")).toBeVisible());
      const save = screen.getByTestId("save-channel");
      expect(screen.getByTestId("save-channel")).toBeDisabled();
      fireEvent.click(save);
      expect(onSave).not.toHaveBeenCalled();
      chooseMethod(method === "Multiple batches" ? "Pilot batch, then remaining" : "Multiple batches");
      expect(screen.getByTestId("save-channel")).toBeEnabled();
      chooseMethod("Single batch");
      expect(screen.getByTestId("save-channel")).toBeEnabled();
      chooseMethod(method);
      expect(screen.getByTestId("save-channel")).toBeDisabled();
      fireEvent.change(screen.getByLabelText(label), { target: { value: "1" } });
      expect(screen.getByLabelText(label)).not.toHaveAttribute("aria-invalid");
      expect(screen.getByTestId("save-channel")).toBeEnabled();
      await applyChannelSettings();
      expect(onSave).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ behavior: expect.objectContaining({ [field]: 1 }) }),
      );
    },
  );

  test("blocks firmware Apply when a staged active size is invalid", async () => {
    const { onSave, onApply } = renderSettings(assignedChannel(), undefined, false, [replacementFile]);
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Multiple batches/ }));
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "0" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    closeChannelSettings();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: /1\.4\.4/ }));
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();
    expect(onSave).not.toHaveBeenCalled();
  });
});

describe("release channel active rollout identity", () => {
  test("replaces stale progress and cancels its miner drilldown when the group reports a new active ID", async () => {
    const current = { ...activeRigRollout, id: 99n, firmwareVersion: "2.0.0", assignmentGeneration: 3n };
    const originalGroup = {
      ...canaryChannel.modelGroups[0],
      activeRolloutId: gatedRigRollout.id,
      assignmentGeneration: gatedRigRollout.assignmentGeneration,
    };
    const originalChannel = { ...canaryChannel, modelGroups: [originalGroup] };
    const group = {
      ...originalGroup,
      activeRolloutId: current.id,
      assignmentGeneration: current.assignmentGeneration,
      firmwareVersion: current.firmwareVersion,
    };
    const channel = { ...originalChannel, modelGroups: [group] };
    const props = {
      ...manageViewProps(),
      channel: originalChannel,
      rollouts: [gatedRigRollout],
    };
    props.listRolloutDevices.mockImplementationOnce(
      (_id, signal) =>
        new Promise<never[]>((resolve) => signal!.addEventListener("abort", () => resolve([]), { once: true })),
    );
    const { rerender } = render(<ReleaseChannelManageView {...props} />);
    expect(within(screen.getByTestId("model-group-Rig")).getByRole("progressbar")).toHaveAccessibleName(
      expect.stringContaining("Review needed"),
    );
    fireEvent.click(screen.getByTestId("view-miners-Rig"));
    await waitFor(() =>
      expect(props.listRolloutDevices).toHaveBeenCalledWith(gatedRigRollout.id, expect.any(AbortSignal)),
    );
    const oldSignal = props.listRolloutDevices.mock.calls[0][1] as AbortSignal;
    rerender(<ReleaseChannelManageView {...props} channel={channel} hasRefreshError />);
    expect(oldSignal.aborted).toBe(true);
    expect(screen.getByTestId("model-group-Rig")).toHaveTextContent("Refreshing update status");
    expect(screen.queryByTestId("model-group-rollout-progress-Rig")).not.toBeInTheDocument();
    expect(screen.getByTestId("view-miners-Rig")).toBeDisabled();
    expect(screen.queryByTestId("channel-update-pill")).not.toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenCalledOnce();
    rerender(<ReleaseChannelManageView {...props} channel={channel} rollouts={[current, gatedRigRollout]} />);
    expect(screen.getByTestId("model-group-rollout-progress-Rig")).toHaveAccessibleName(
      expect.stringContaining("Updating to 2.0.0"),
    );
    expect(screen.getByRole("table", { name: "Assigned miners by model" }).querySelectorAll("tbody tr")).toHaveLength(
      1,
    );
    expect(screen.getByTestId("channel-update-pill")).toHaveTextContent("Update in progress");
    expect(screen.getByTestId("view-miners-Rig")).toBeEnabled();
    await waitFor(() => expect(props.listRolloutDevices).toHaveBeenLastCalledWith(current.id, expect.any(AbortSignal)));
    rerender(
      <ReleaseChannelManageView
        {...props}
        channel={{ ...channel, modelGroups: [{ ...group, activeRolloutId: 0n }] }}
        rollouts={[current, gatedRigRollout]}
      />,
    );
    expect(screen.queryByTestId("model-group-rollout-progress-Rig")).not.toBeInTheDocument();
    expect(screen.queryByTestId("channel-update-pill")).not.toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(2);
  });
});

describe("release channel pacing guidance", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });

  afterEach(() => vi.restoreAllMocks());

  test.each([
    [RolloutMethod.BATCHED, "~2 batches of 10 across 20 miners"],
    [RolloutMethod.PILOT_THEN_CONTINUE, "Pilot batch of 3, then 17 remaining"],
    [RolloutMethod.ALL_AT_ONCE, "20 miners in a single batch"],
  ] as const)("does not combine models into a scope-wide schedule for method %s", (method, aggregatePlan) => {
    const channel = assignedChannel();
    channel.minerCount = 20;
    channel.behavior = create(RolloutBehaviorSchema, { method, batchSize: 10, pilotSize: 3 });
    channel.modelGroups = [
      create(ReleaseChannelModelGroupSchema, { ...channel.modelGroups[0], minerCount: 10 }),
      create(ReleaseChannelModelGroupSchema, { manufacturer: "Bitmain", model: "S21", minerCount: 10 }),
    ];
    renderSettings(channel);
    const controls = screen.getByTestId("rollout-controls");
    expect(controls).not.toHaveTextContent(aggregatePlan);
    expect(
      screen.getByText(
        "Batch and pilot sizes apply separately to each model. The offline limit is shared across the channel.",
      ),
    ).toBeInTheDocument();
    if (method === RolloutMethod.BATCHED) expect(screen.getByLabelText("Batch size (miners)")).toHaveValue("10");
    if (method === RolloutMethod.PILOT_THEN_CONTINUE)
      expect(screen.getByLabelText("Pilot batch size (miners)")).toHaveValue("3");

    // Staging only a clear must not turn the whole scope into a predicted update.
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("1");
    expect(screen.getByTestId("rollout-controls")).not.toHaveTextContent(aggregatePlan);
  });

  test("does not treat one model or a draft scope preview as rollout targets", async () => {
    const channel = assignedChannel();
    channel.minerCount = 10;
    channel.behavior = create(RolloutBehaviorSchema, { method: RolloutMethod.BATCHED, batchSize: 5 });
    channel.modelGroups = [
      create(ReleaseChannelModelGroupSchema, { ...channel.modelGroups[0], minerCount: 6, onTargetCount: 6 }),
      create(ReleaseChannelModelGroupSchema, {
        ...channel.modelGroups[0],
        manufacturer: " PROTO ",
        model: " rig ",
        minerCount: 4,
        onTargetCount: 4,
      }),
    ];
    const { previewScope } = renderSettings(channel);
    const controls = screen.getByTestId("rollout-controls");
    expect(controls).not.toHaveTextContent("~2 batches of 5 across 10 miners");
    previewScope.mockResolvedValue(
      create(PreviewReleaseChannelScopeResponseSchema, {
        minerCount: 60,
        modelCount: 2,
        models: [
          { manufacturer: "Proto", model: "Rig", minerCount: 30 },
          { manufacturer: "Bitmain", model: "S21", minerCount: 30 },
        ],
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 2" }));
    await waitFor(() =>
      expect(previewScope).toHaveBeenLastCalledWith(
        create(ReleaseChannelScopeSchema, { siteIds: [2n] }),
        channel.id,
        expect.any(AbortSignal),
      ),
    );
    expect(screen.queryByTestId("scope-preview")).not.toBeInTheDocument();
    expect(controls).not.toHaveTextContent("~12 batches of 5 across 60 miners");
    expect(controls).not.toHaveTextContent("~2 batches of 5 across 10 miners");
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue("5");
  });
});

describe("release channel write ordering", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });

  afterEach(() => vi.restoreAllMocks());

  const stageReplacement = () => {
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: /1\.4\.4/ }));
  };

  const chooseBatches = (batchSize: string) => {
    openChannelSettings();
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Multiple batches/ }));
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: batchSize } });
  };

  test("reviews both drafts and saves settings before starting firmware under one write lock", async () => {
    const channel = assignedChannel();
    const write = deferredWrite();
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockReturnValueOnce(write.promise);
    const { onApply } = renderManage(channel, onSave, false, [replacementFile]);
    chooseBatches("3");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Canary" } });
    closeChannelSettings();
    stageReplacement();
    openChannelSettings();
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue("3");
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(onSave).not.toHaveBeenCalled();
    expect(onApply).not.toHaveBeenCalled();
    const dialog = screen.getByTestId("apply-firmware-dialog");
    expect(dialog).toHaveTextContent("Channel settings");
    expect(dialog).toHaveTextContent("Canary");
    expect(dialog).toHaveTextContent("Pacing: batches of 3, back to back.");
    expect(dialog).toHaveTextContent("1.4.4");
    const apply = within(dialog).getByRole("button", { name: "Apply changes" });
    fireEvent.click(apply);
    fireEvent.click(apply);
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ name: "Canary", behavior: expect.objectContaining({ batchSize: 3 }) }),
    );
    expect(onApply).not.toHaveBeenCalled();
    expect(screen.getByTestId("channel-settings")).toBeDisabled();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toBeDisabled();
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Discard" })).toBeDisabled();
    await act(async () => write.resolve());
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "replacement" },
    ]);
    expect(onSave.mock.invocationCallOrder[0]).toBeLessThan(onApply.mock.invocationCallOrder[0]);
    await waitFor(() => expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument());
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
  });

  test("keeps both drafts retryable after settings rejection and never starts firmware first", async () => {
    const write = deferredWrite();
    const onSave = vi
      .fn<(draft: ReleaseChannelDraft) => Promise<void>>()
      .mockReturnValueOnce(write.promise)
      .mockResolvedValue(undefined);
    const { onApply } = renderManage(assignedChannel(), onSave, true, [replacementFile]);
    chooseBatches("7");
    closeChannelSettings();
    stageReplacement();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const dialog = screen.getByTestId("apply-firmware-dialog");
    const apply = within(dialog).getByRole("button", { name: "Apply changes" });
    fireEvent.click(apply);
    expect(apply).toBeDisabled();
    await act(async () => write.reject(new Error("Settings rejected")));
    expect(onApply).not.toHaveBeenCalled();
    expect(apply).toBeEnabled();
    expect(dialog).toHaveTextContent("Pacing: batches of 7, back to back.");
    expect(pushToast).toHaveBeenCalledWith({ message: "Settings rejected", status: "error" });
    await act(async () => fireEvent.click(apply));
    expect(onSave).toHaveBeenCalledTimes(2);
    expect(onSave.mock.calls[1]).toEqual(onSave.mock.calls[0]);
    expect(onApply).toHaveBeenCalledOnce();
  });

  test("retains acknowledged settings after firmware fails and retries only the pending firmware", async () => {
    const channel = assignedChannel();
    const { onApply, onSave, updateChannel } = renderManage(channel, undefined, true, [replacementFile]);
    const write = deferredWrite();
    onApply.mockReturnValueOnce(write.promise);
    chooseBatches("4");
    closeChannelSettings();
    stageReplacement();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const dialog = screen.getByTestId("apply-firmware-dialog");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Apply changes" })));
    expect(onSave).toHaveBeenCalledOnce();
    expect(onApply).toHaveBeenCalledOnce();
    expect(screen.getByTestId("channel-settings")).toBeDisabled();
    await act(async () => write.reject(new Error("Firmware rejected")));
    expect(dialog).toHaveTextContent(
      "Channel settings were saved, but firmware changes could not be applied. Your firmware selections are still pending. Firmware rejected",
    );
    expect(dialog).toHaveTextContent("Firmware rejected");
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("1");
    expect(dialog).toHaveTextContent("1 change pending");
    expect(screen.queryByText("Channel settings changes pending")).not.toBeInTheDocument();
    updateChannel({ ...channel }, true);
    expect(dialog).toHaveTextContent("Pacing: batches of 4, back to back.");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Start update" })));
    expect(onSave).toHaveBeenCalledOnce();
    expect(onApply).toHaveBeenCalledTimes(2);
    expect(onApply.mock.calls[1]).toEqual(onApply.mock.calls[0]);
    openChannelSettings();
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue("4");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  test("stages firmware for a model introduced by draft scope and applies scope first", async () => {
    const newFile = { ...replacementFile, id: "other-firmware", target_model: "Other" };
    const channel = assignedChannel();
    const { previewScope, onSave, onApply } = renderManage(channel, undefined, false, [newFile]);
    previewScope.mockResolvedValue(
      create(PreviewReleaseChannelScopeResponseSchema, {
        minerCount: 3,
        modelCount: 2,
        models: [
          { manufacturer: "Proto", model: "Rig", minerCount: 1 },
          { manufacturer: "Proto", model: "Other", minerCount: 2 },
        ],
      }),
    );
    openChannelSettings();
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 2" }));
    await waitFor(() => expect(screen.getByTestId("channel-firmware-select-Other")).toBeInTheDocument());
    closeChannelSettings();
    const row = screen.getByTestId("channel-firmware-select-Other").closest("tr")!;
    expect(row).not.toHaveTextContent("Pending scope change");
    expect(within(row).getByTestId("view-miners-Other")).toBeDisabled();
    fireEvent.click(within(row).getByTestId("channel-firmware-select-Other"));
    fireEvent.click(screen.getByRole("option", { name: /1.4.4/ }));
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("2");
    expect(screen.queryByText(/changes? pending/i)).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const dialog = screen.getByTestId("apply-firmware-dialog");
    expect(dialog).toHaveTextContent("Applies to");
    expect(dialog).toHaveTextContent("Proto Other");
    expect(onSave).not.toHaveBeenCalled();
    expect(onApply).not.toHaveBeenCalled();
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Apply changes" })));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ scope: expect.objectContaining({ siteIds: [2n] }) }),
    );
    expect(onApply).toHaveBeenCalledExactlyOnceWith(channel.id, [
      { manufacturer: "Proto", model: "Other", firmwareFileId: "other-firmware" },
    ]);
    expect(onSave.mock.invocationCallOrder[0]).toBeLessThan(onApply.mock.invocationCallOrder[0]);
  });

  test("retains a staged model when a scope save removes its row and firmware needs retry", async () => {
    const channel = assignedChannel();
    channel.modelGroups = channel.modelGroups.map((group) => ({
      ...group,
      firmwareFileId: "",
      firmwareChecksum: "",
      firmwareVersion: "",
      assignmentGeneration: 0n,
    }));
    const { onApply, onSave, updateChannel } = renderManage(channel, undefined, false, [replacementFile]);
    openChannelSettings();
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Clear sites" }));
    closeChannelSettings();
    stageReplacement();
    onSave.mockImplementationOnce(async (draft) => {
      updateChannel({ ...channel, ...draft, modelGroups: [], minerCount: 0 }, true);
    });
    onApply.mockRejectedValueOnce(new Error("Retry firmware"));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const dialog = screen.getByTestId("apply-firmware-dialog");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Apply changes" })));
    expect(onSave).toHaveBeenCalledOnce();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("1.4.4");
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("1");
    expect(dialog).toHaveTextContent("Retry firmware");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Start update" })));
    expect(onSave).toHaveBeenCalledOnce();
    expect(onApply).toHaveBeenCalledTimes(2);
    expect(onApply.mock.calls[1]).toEqual(onApply.mock.calls[0]);
    expect(onApply.mock.calls[1][1]).toEqual([{ manufacturer: "Proto", model: "Rig", firmwareFileId: "replacement" }]);
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
  });
});

describe("release channel firmware assignments", () => {
  beforeEach(() => {
    // Give the real portal a visible anchor in jsdom's otherwise empty layout.
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });

  afterEach(() => vi.restoreAllMocks());

  test("stages and applies an explicit clear for an assignment whose uploaded file is unavailable", async () => {
    const channel = assignedChannel();
    const { onApply, updateChannel } = renderManage(channel);
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    expect(picker).toHaveTextContent("1.4.3");
    expect(picker).not.toHaveTextContent("No firmware");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    fireEvent.click(picker);
    const clear = screen.getByRole("option", { name: "No firmware" });
    expect(clear).toHaveAttribute("aria-selected", "false");
    fireEvent.click(clear);
    expect(picker).toHaveTextContent("No firmware");
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("1");

    updateChannel(channel, true);
    expect(picker).toHaveTextContent("No firmware");
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("No firmware");
    fireEvent.click(screen.getByRole("button", { name: "Clear assignments" }));
    await waitFor(() =>
      expect(onApply).toHaveBeenCalledExactlyOnceWith(channel.id, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: "" },
      ]),
    );
    await waitFor(() => expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument());
  });

  test("discarding a staged clear restores both the unavailable assignment and channel settings", () => {
    renderSettings(assignedChannel());
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved name" } });
    closeChannelSettings();
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    fireEvent.click(picker);
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(picker).toHaveTextContent("1.4.3");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Production");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  test("does not stage a clear when the model has no assignment", () => {
    const channel = existingChannel();
    channel.modelGroups = [create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model: "Rig" })];
    const { onApply } = renderManage(channel);
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    expect(picker).toHaveTextContent("No firmware");
    fireEvent.click(picker);
    expect(screen.getByRole("option", { name: "No firmware" })).toHaveAttribute("aria-selected", "true");
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();
  });

  test.each([
    ["missing", []],
    ["mismatched", [{ ...replacementFile, target_model: "Other model" }]],
    ["edited", [replacementFile]],
    ["missing version", [{ ...replacementFile, firmware_version: undefined }]],
  ] as const)("preserves the assigned version when uploaded file metadata is %s", (_, files) => {
    renderManage(assignedChannel("replacement"), undefined, false, [...files]);
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    expect(picker).toHaveTextContent("1.4.3");
    expect(picker).not.toHaveTextContent("No firmware");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    fireEvent.click(picker);
    expect(screen.getByRole("option", { name: "No firmware" })).toHaveAttribute("aria-selected", "false");
  });

  test.each([
    { reason: "missing", version: undefined },
    { reason: "empty", version: "" },
    { reason: "ASCII whitespace", version: " \t\n " },
    { reason: "NEL whitespace", version: "\u0085" },
    { reason: "Unicode whitespace", version: "\u2003\u3000" },
    { reason: "null character", version: "1.4\u0000.4" },
    { reason: "over 255 code points", version: "🚀".repeat(256) },
  ])("does not offer firmware with a $reason version as an assignable file", ({ version }) => {
    const { onApply } = renderManage(assignedChannel(), undefined, false, [
      { ...replacementFile, firmware_version: version },
    ]);
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    expect(screen.getAllByRole("option")).toHaveLength(1);
    expect(screen.getByRole("option", { name: "No firmware" })).toBeVisible();
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();
  });

  test.each([
    { reason: "255 Unicode code points after trimming", version: `\u0085${"🚀".repeat(255)}\u3000` },
    { reason: "Unicode version", version: "版本-🚀" },
    { reason: "BOM preserved by Go", version: "\uFEFF" },
    { reason: "NEL around a version", version: "\u00851.4.4\u0085" },
    { reason: "an internal non-null control", version: "1.4\u0001.4" },
  ])("accepts firmware with $reason", async ({ version }) => {
    const { onApply } = renderManage(assignedChannel(), undefined, false, [
      { ...replacementFile, firmware_version: version },
    ]);
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(2);
    fireEvent.click(options[1]);
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "replacement" },
    ]);
  });

  test("allows clearing a saved assignment after its upload loses version metadata", async () => {
    const { onApply } = renderManage(assignedChannel("replacement"), undefined, false, [
      { ...replacementFile, firmware_version: undefined },
    ]);
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    expect(picker).toHaveTextContent("1.4.3");
    fireEvent.click(picker);
    const clear = screen.getByRole("option", { name: "No firmware" });
    expect(clear).toHaveAttribute("aria-selected", "false");
    expect(screen.getAllByRole("option")).toHaveLength(1);
    fireEvent.click(clear);
    expect(picker).toHaveTextContent("No firmware");
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Clear assignments" })));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [{ manufacturer: "Proto", model: "Rig", firmwareFileId: "" }]);
  });

  test("keeps the assignment snapshot in the open picker through catalog edits, staging, and discard", () => {
    const otherFile = { ...replacementFile, id: "different-payload", firmware_version: "1.4.5" };
    const { updateFirmwareFiles, onApply } = renderManage(assignedChannel("replacement"), undefined, false, [
      { ...replacementFile, firmware_version: "1.4.3" },
      otherFile,
    ]);
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    fireEvent.click(picker);
    updateFirmwareFiles([replacementFile, otherFile]);

    expect(picker).toHaveTextContent("1.4.3");
    expect(screen.getByRole("option", { name: /^1\.4\.3/ })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("option", { name: /^1\.4\.4/ })).not.toBeInTheDocument();
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("option", { name: /^1\.4\.5/ }));
    expect(picker).toHaveTextContent("1.4.5");
    fireEvent.click(picker);
    expect(screen.getByRole("option", { name: /^1\.4\.3/ })).toHaveAttribute("aria-selected", "false");
    fireEvent.click(picker);
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(picker).toHaveTextContent("1.4.3");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();
  });

  test("keeps a staged replacement explicit when its file metadata disappears", () => {
    const { updateFirmwareFiles } = renderManage(assignedChannel(), undefined, false, [replacementFile]);
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    fireEvent.click(picker);
    fireEvent.click(screen.getByRole("option", { name: /1.4.4/ }));
    expect(picker).toHaveTextContent("1.4.4");
    updateFirmwareFiles([]);
    expect(picker).toHaveTextContent("Firmware details unavailable");
    expect(picker).not.toHaveTextContent("No firmware");
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    expect(screen.getByText(/Selected firmware is unavailable for this model/)).toBeInTheDocument();
  });

  test.each(["deleted", "retargeted"])(
    "blocks the whole staged update when a selected file is %s, then recovers after reselection",
    async (change) => {
      const channel = existingChannel();
      channel.modelGroups = ["Rig", "Other"].map((model) =>
        create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model, minerCount: 1 }),
      );
      const otherFile = { ...replacementFile, id: "other-file", target_model: "Other" };
      const recoveredFile = { ...replacementFile, id: "new-rig-file", firmware_version: "1.4.5" };
      const { onApply, onSave, updateFirmwareFiles } = renderManage(channel, undefined, false, [
        replacementFile,
        otherFile,
      ]);
      openChannelSettings();
      for (const model of ["Rig", "Other"]) {
        fireEvent.click(screen.getByTestId(`channel-firmware-select-${model}`));
        fireEvent.click(screen.getByRole("option", { name: /1.4.4/ }));
      }
      const apply = screen.getByTestId("apply-firmware-changes");
      fireEvent.click(apply);
      const start = screen.getByRole("button", { name: "Start update" });
      expect(start).toBeEnabled();

      updateFirmwareFiles([
        otherFile,
        recoveredFile,
        ...(change === "retargeted" ? [{ ...replacementFile, target_model: "Other" }] : []),
      ]);
      expect(screen.getByTestId("pending-change-count")).toHaveTextContent("2");
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("Firmware details unavailable");
      expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent(
        "Choose valid firmware for every changed model or discard the pending changes.",
      );
      expect(apply).toBeDisabled();
      expect(start).toBeDisabled();
      fireEvent.click(start);
      expect(onApply).not.toHaveBeenCalled();
      fireEvent.click(within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Cancel" }));
      openChannelSettings();
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Combined settings change" } });
      expect(screen.getByTestId("save-channel")).toBeDisabled();
      fireEvent.click(screen.getByTestId("save-channel"));
      expect(onSave).not.toHaveBeenCalled();
      closeChannelSettings();

      fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
      fireEvent.click(screen.getByRole("option", { name: /1.4.5/ }));
      expect(apply).toBeEnabled();
      fireEvent.click(apply);
      await act(async () =>
        fireEvent.click(
          within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" }),
        ),
      );
      expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Combined settings change" }));
      expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: "new-rig-file" },
        { manufacturer: "Proto", model: "Other", firmwareFileId: "other-file" },
      ]);
    },
  );

  test.each([
    { reason: "missing", version: undefined },
    { reason: "Go whitespace only", version: "\u0085\u3000" },
    { reason: "null character", version: "1.4\u0000.4" },
    { reason: "too many code points", version: "界".repeat(256) },
  ])(
    "blocks an open confirmation when a staged version becomes $reason and recovers on refresh",
    async ({ version }) => {
      const channel = existingChannel();
      channel.modelGroups = ["Rig", "Other"].map((model) =>
        create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model, minerCount: 1 }),
      );
      const otherFile = { ...replacementFile, id: "other-file", target_model: "Other" };
      const { onApply, updateFirmwareFiles } = renderManage(channel, undefined, false, [replacementFile, otherFile]);
      for (const model of ["Rig", "Other"]) {
        fireEvent.click(screen.getByTestId(`channel-firmware-select-${model}`));
        fireEvent.click(screen.getByRole("option", { name: /1.4.4/ }));
      }
      const apply = screen.getByTestId("apply-firmware-changes");
      fireEvent.click(apply);
      const start = screen.getByRole("button", { name: "Start update" });
      expect(start).toBeEnabled();
      updateFirmwareFiles([{ ...replacementFile, firmware_version: version }, otherFile]);
      expect(screen.getByTestId("pending-change-count")).toHaveTextContent("2");
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("Firmware details unavailable");
      expect(screen.getByTestId("channel-firmware-select-Other")).toHaveTextContent("1.4.4");
      expect(apply).toBeDisabled();
      expect(start).toBeDisabled();
      expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent(
        "Choose valid firmware for every changed model or discard the pending changes.",
      );
      fireEvent.click(start);
      expect(onApply).not.toHaveBeenCalled();

      updateFirmwareFiles([replacementFile, otherFile]);
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("1.4.4");
      expect(apply).toBeEnabled();
      expect(start).toBeEnabled();
      await act(async () => fireEvent.click(start));
      expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: "replacement" },
        { manufacturer: "Proto", model: "Other", firmwareFileId: "other-file" },
      ]);
    },
  );

  test("retains each unassigned model's staged choice after all selected files disappear and allows discard", () => {
    const channel = existingChannel();
    channel.modelGroups = ["Rig", "Other"].map((model) =>
      create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model, minerCount: 1 }),
    );
    const { onApply, updateFirmwareFiles } = renderManage(channel, undefined, false, [
      replacementFile,
      { ...replacementFile, id: "other-file", target_model: "Other" },
    ]);
    for (const model of ["Rig", "Other"]) {
      fireEvent.click(screen.getByTestId(`channel-firmware-select-${model}`));
      fireEvent.click(screen.getByRole("option", { name: /1.4.4/ }));
    }
    updateFirmwareFiles([]);
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("2");
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(screen.queryByText(/Selected firmware is unavailable for this model/)).not.toBeInTheDocument();
    for (const model of ["Rig", "Other"]) {
      expect(screen.getByTestId(`channel-firmware-select-${model}`)).toHaveTextContent("No firmware");
    }
    expect(onApply).not.toHaveBeenCalled();
  });

  test("accepts catalog target spelling changes that retain the canonical model", async () => {
    const { onApply, updateFirmwareFiles } = renderManage(assignedChannel(), undefined, false, [replacementFile]);
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: /1.4.4/ }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    updateFirmwareFiles([{ ...replacementFile, target_manufacturer: " PROTO ", target_model: " rig " }]);
    expect(screen.queryByText(/Selected firmware is unavailable for this model/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start update" })).toBeEnabled();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "PROTO", model: "rig", firmwareFileId: "replacement" },
    ]);
  });

  test.each(
    (["manufacturer", "model"] as const).flatMap((field) =>
      [
        { reason: "Unicode", value: "Prötö" },
        { reason: "internal control", value: "A\u0001B" },
        { reason: "overlong", value: "A".repeat(256) },
        { reason: "empty", value: " \t " },
      ].map(({ reason, value }) => ({ field, reason, value })),
    ),
  )("keeps an unsupported $reason $field visible but filters its firmware options", ({ field, value }) => {
    const channel = existingChannel();
    const group = create(ReleaseChannelModelGroupSchema, {
      manufacturer: "Proto",
      model: "Rig",
      [field]: value,
      minerCount: 1,
    });
    channel.modelGroups = [group];
    const catalogField = field === "manufacturer" ? "target_manufacturer" : "target_model";
    const { onApply } = renderManage(channel, undefined, false, [{ ...replacementFile, [catalogField]: value }]);
    const row = screen.getByTestId(`model-group-${group.model}`, { normalizer: (text) => text });
    expect(row).toBeVisible();
    expect(within(row).getByRole("alert")).toHaveTextContent("1–255 printable ASCII characters");
    expect(within(row).getByRole("alert")).toHaveTextContent("Correct the miner's reported identity");
    fireEvent.click(screen.getByTestId(`channel-firmware-select-${group.model}`, { normalizer: (text) => text }));
    expect(screen.queryByRole("option", { name: /1\.4\.4/ })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();
  });

  test.each(["catalog", "observed"])(
    "does not erase a leading BOM from the %s target to match an ASCII identity",
    (source) => {
      const channel = existingChannel();
      channel.modelGroups = [
        create(ReleaseChannelModelGroupSchema, {
          manufacturer: source === "observed" ? "\uFEFFProto" : "Proto",
          model: "Rig",
        }),
      ];
      const { onApply } = renderManage(channel, undefined, false, [
        {
          ...replacementFile,
          target_manufacturer: source === "catalog" ? "\uFEFFProto" : "Proto",
        },
      ]);
      fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
      expect(screen.queryByRole("option", { name: /1\.4\.4/ })).not.toBeInTheDocument();
      expect(screen.getByRole("option", { name: "No firmware" })).toBeInTheDocument();
      expect(onApply).not.toHaveBeenCalled();
    },
  );

  test.each(["manufacturer", "model"] as const)(
    "accepts 255 printable ASCII characters, punctuation, and internal spaces in %s",
    async (field) => {
      const target = `A !~${"B".repeat(251)}`;
      const channel = existingChannel();
      const group = create(ReleaseChannelModelGroupSchema, {
        manufacturer: "Proto",
        model: "Rig",
        [field]: ` ${target.toLowerCase()} `,
        minerCount: 1,
      });
      channel.modelGroups = [group];
      const catalogField = field === "manufacturer" ? "target_manufacturer" : "target_model";
      const { onApply } = renderManage(channel, undefined, false, [
        { ...replacementFile, [catalogField]: ` \t${target} \n` },
      ]);
      expect(screen.queryByText(/1–255 printable ASCII characters/)).not.toBeInTheDocument();
      fireEvent.click(screen.getByTestId(`channel-firmware-select-${group.model}`, { normalizer: (text) => text }));
      fireEvent.click(screen.getByRole("option", { name: /1\.4\.4/ }));
      fireEvent.click(screen.getByTestId("apply-firmware-changes"));
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
      expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
        { manufacturer: "Proto", model: "Rig", [field]: target, firmwareFileId: "replacement" },
      ]);
    },
  );

  test("keeps a BOM identity separate when staging and applying a valid model beside it", async () => {
    const channel = existingChannel();
    channel.modelGroups = ["Rig", "\uFEFFRig"].map((model) =>
      create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model, minerCount: 1 }),
    );
    const { onApply } = renderManage(channel, undefined, false, [replacementFile]);
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: /1\.4\.4/ }));
    expect(screen.getByTestId("channel-firmware-select-\uFEFFRig", { normalizer: (text) => text })).toHaveTextContent(
      "No firmware",
    );
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("1");
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "replacement" },
    ]);
  });

  test("joins NEL-padded observed and catalog targets and submits their trimmed identity", async () => {
    const channel = existingChannel();
    channel.modelGroups = [
      create(ReleaseChannelModelGroupSchema, { manufacturer: "\u0085proto", model: "rig\u0085", minerCount: 1 }),
    ];
    const { onApply } = renderManage(channel, undefined, false, [
      { ...replacementFile, target_manufacturer: "Proto\u0085", target_model: "\u0085Rig" },
    ]);
    expect(screen.queryByText(/1–255 printable ASCII characters/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("channel-firmware-select-rig\u0085", { normalizer: (text) => text }));
    fireEvent.click(screen.getByRole("option", { name: /1\.4\.4/ }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "replacement" },
    ]);
  });

  test("clears using the trimmed saved assignment target while preserving raw observed aliases", async () => {
    const channel = assignedChannel();
    channel.modelGroups[0] = {
      ...channel.modelGroups[0],
      manufacturer: " PROTO ",
      model: " rig ",
      firmwareTargetManufacturer: " Proto ",
      firmwareTargetModel: " Rig ",
    };
    const { onApply } = renderManage(channel);
    fireEvent.click(screen.getByTestId("channel-firmware-select- rig ", { normalizer: (text) => text }));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Clear assignments" })));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [{ manufacturer: "Proto", model: "Rig", firmwareFileId: "" }]);
  });

  test.each(["catalog target", "saved clear target"])(
    "blocks an open confirmation when the %s becomes unsupported without dropping another staged model",
    async (change) => {
      const channel = assignedChannel();
      channel.modelGroups.push(create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model: "Other" }));
      const otherFile = { ...replacementFile, id: "other-file", target_model: "Other", firmware_version: "2.0.0" };
      const files = [replacementFile, otherFile];
      const { onApply, updateFirmwareFiles, updateChannel } = renderManage(channel, undefined, false, files);
      fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
      fireEvent.click(
        screen.getByRole("option", { name: change === "saved clear target" ? "No firmware" : /1\.4\.4/ }),
      );
      fireEvent.click(screen.getByTestId("channel-firmware-select-Other"));
      fireEvent.click(screen.getByRole("option", { name: /2\.0\.0/ }));
      fireEvent.click(screen.getByTestId("apply-firmware-changes"));
      const start = within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", {
        name: change === "saved clear target" ? "Apply changes" : "Start update",
      });
      expect(start).toBeEnabled();

      if (change === "catalog target") {
        updateFirmwareFiles([{ ...replacementFile, target_model: "Ríg" }, otherFile]);
      } else {
        updateChannel(
          {
            ...channel,
            modelGroups: [
              { ...channel.modelGroups[0], firmwareTargetManufacturer: "\uFEFFProto" },
              channel.modelGroups[1],
            ],
          },
          false,
        );
      }
      expect(screen.getByTestId("pending-change-count")).toHaveTextContent("2");
      expect(screen.getByTestId("channel-firmware-select-Other")).toHaveTextContent("2.0.0");
      expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
      expect(start).toBeDisabled();
      const row = screen.getByTestId("model-group-Rig");
      expect(
        within(row).getByText(
          change === "catalog target" ? /Selected firmware is unavailable/ : /Correct the target identity or discard/,
        ),
      ).toHaveAttribute("role", "alert");
      fireEvent.click(start);
      expect(onApply).not.toHaveBeenCalled();

      if (change === "catalog target") updateFirmwareFiles(files);
      else updateChannel(channel, false);
      expect(screen.getByTestId("pending-change-count")).toHaveTextContent("2");
      expect(start).toBeEnabled();
      await act(async () => fireEvent.click(start));
      expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: change === "saved clear target" ? "" : "replacement" },
        { manufacturer: "Proto", model: "Other", firmwareFileId: "other-file" },
      ]);
    },
  );

  test.each(["assign", "clear"])(
    "sends one %s per canonical pair while preserving distinct model assignments",
    async (action) => {
      const channel = assignedChannel();
      channel.modelGroups.push(
        create(ReleaseChannelModelGroupSchema, { ...channel.modelGroups[0], manufacturer: " PROTO ", model: " rig " }),
        create(ReleaseChannelModelGroupSchema, {
          ...channel.modelGroups[0],
          manufacturer: "BITMAIN",
          model: "S21",
          firmwareTargetManufacturer: "Bitmain",
          firmwareTargetModel: "S21",
        }),
      );
      const s21File = { ...replacementFile, id: "s21-file", target_manufacturer: "Bitmain", target_model: "S21" };
      const { onApply } = renderManage(channel, undefined, false, [replacementFile, s21File]);
      for (const model of ["Rig", "S21"]) {
        fireEvent.click(screen.getByTestId(`channel-firmware-select-${model}`));
        fireEvent.click(screen.getByRole("option", { name: action === "clear" ? "No firmware" : /1.4.4/ }));
      }
      expect(screen.getByTestId("channel-firmware-select- rig ", { normalizer: (value) => value })).toHaveTextContent(
        action === "clear" ? "No firmware" : "1.4.4",
      );
      expect(screen.getByTestId("pending-change-count")).toHaveTextContent("2");
      fireEvent.click(screen.getByTestId("apply-firmware-changes"));
      fireEvent.click(screen.getByRole("button", { name: action === "clear" ? "Clear assignments" : "Start update" }));
      await waitFor(() =>
        expect(onApply).toHaveBeenCalledExactlyOnceWith(channel.id, [
          { manufacturer: "Proto", model: "Rig", firmwareFileId: action === "clear" ? "" : "replacement" },
          { manufacturer: "Bitmain", model: "S21", firmwareFileId: action === "clear" ? "" : "s21-file" },
        ]),
      );
      await waitFor(() => expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument());
    },
  );

  test("opens the miners for the exact observed row when canonical pairs are shared", async () => {
    const channel = assignedChannel();
    channel.modelGroups.push(
      create(ReleaseChannelModelGroupSchema, { ...channel.modelGroups[0], manufacturer: " PROTO ", model: " rig " }),
    );
    const { listChannelMiners } = renderManage(channel);
    fireEvent.click(screen.getByTestId("view-miners- rig ", { normalizer: (value) => value }));
    await waitFor(() =>
      expect(listChannelMiners).toHaveBeenCalledExactlyOnceWith(1n, " PROTO ", " rig ", expect.any(AbortSignal)),
    );
  });
});

describe("effective release channel behavior", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });

  afterEach(() => vi.restoreAllMocks());

  const reviewedBatches = () =>
    create(RolloutBehaviorSchema, {
      method: RolloutMethod.BATCHED,
      order: RolloutOrder.RANDOM,
      batchSize: 7,
      reviewAfterEachBatch: true,
      autoContinueOnHealthyTelemetry: true,
      stabilizationSeconds: 600,
      thresholds: { maxHashrateDropPercent: 10, maxNewErrors: 0 },
      maxConcurrentOffline: 4,
    });

  test.each([
    {
      name: "single batch",
      change: () => {
        fireEvent.click(screen.getByTestId("rollout-method"));
        fireEvent.click(screen.getByRole("option", { name: /^Single batch/ }));
      },
      saved: () =>
        create(RolloutBehaviorSchema, {
          method: RolloutMethod.ALL_AT_ONCE,
          order: RolloutOrder.RANDOM,
          maxConcurrentOffline: 4,
        }),
    },
    {
      name: "unreviewed batches",
      change: () => fireEvent.click(screen.getByLabelText("Review after each batch")),
      saved: () =>
        create(RolloutBehaviorSchema, {
          method: RolloutMethod.BATCHED,
          order: RolloutOrder.RANDOM,
          batchSize: 7,
          maxConcurrentOffline: 4,
        }),
    },
    {
      name: "manual review",
      change: () => fireEvent.click(screen.getByLabelText("Auto-continue healthy batches")),
      saved: () =>
        create(RolloutBehaviorSchema, {
          method: RolloutMethod.BATCHED,
          order: RolloutOrder.RANDOM,
          batchSize: 7,
          reviewAfterEachBatch: true,
          maxConcurrentOffline: 4,
        }),
    },
  ])("becomes clean after saving $name while retaining hidden form values", async ({ change, saved }) => {
    const channel = { ...assignedChannel(), behavior: reviewedBatches() };
    const { onSave, updateChannel } = renderSettings(channel);

    expect(screen.getByTestId("save-channel")).toBeDisabled();
    change();
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await applyChannelSettings();
    expect(onSave).toHaveBeenCalledOnce();
    // The form keeps values for switching back; serialization strips them.
    expect(onSave.mock.calls[0][0].behavior.thresholds).toBeUndefined();
    updateChannel({ ...channel, behavior: saved() }, false);
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Max miners offline at once (0 for no limit)"), { target: { value: "5" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Max miners offline at once (0 for no limit)"), { target: { value: "4" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(screen.getByTestId("apply-firmware-dialog")).not.toHaveTextContent("Unsaved channel changes");
  });

  test("ignores retained batch delays and the server's implicit pilot review flag", async () => {
    const channel = {
      ...existingChannel(),
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.BATCHED,
        order: RolloutOrder.RANDOM,
        batchSize: 7,
        waitBetweenBatchesSeconds: 120,
      }),
    };
    const { onSave, updateChannel } = renderSettings(channel);

    fireEvent.click(screen.getByLabelText("Review after each batch"));
    await applyChannelSettings();
    expect(onSave.mock.calls[0][0].behavior.waitBetweenBatchesSeconds).toBe(0);
    updateChannel(
      {
        ...channel,
        behavior: create(RolloutBehaviorSchema, {
          method: RolloutMethod.BATCHED,
          order: RolloutOrder.RANDOM,
          batchSize: 7,
          reviewAfterEachBatch: true,
        }),
      },
      false,
    );
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByLabelText("Review after each batch"));
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Pilot batch/ }));
    fireEvent.change(screen.getByLabelText("Pilot batch size (miners)"), { target: { value: "2" } });
    await applyChannelSettings();
    updateChannel(
      {
        ...channel,
        behavior: create(RolloutBehaviorSchema, {
          method: RolloutMethod.PILOT_THEN_CONTINUE,
          order: RolloutOrder.RANDOM,
          pilotSize: 2,
          reviewAfterEachBatch: true,
        }),
      },
      false,
    );
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  test("treats empty thresholds as absent while preserving an explicit zero limit", async () => {
    const behavior = create(RolloutBehaviorSchema, {
      method: RolloutMethod.BATCHED,
      order: RolloutOrder.RANDOM,
      batchSize: 7,
      reviewAfterEachBatch: true,
      autoContinueOnHealthyTelemetry: true,
      stabilizationSeconds: 600,
      thresholds: { maxHashrateDropPercent: 10 },
    });
    const channel = { ...existingChannel(), behavior };
    const { updateChannel } = renderSettings(channel);

    fireEvent.change(screen.getByLabelText("Max hashrate drop (%)"), { target: { value: "" } });
    await applyChannelSettings();
    updateChannel(
      { ...channel, behavior: create(RolloutBehaviorSchema, { ...behavior, thresholds: undefined }) },
      false,
    );
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Max errors"), { target: { value: "0" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await applyChannelSettings();
    updateChannel(
      {
        ...channel,
        behavior: create(RolloutBehaviorSchema, {
          ...behavior,
          thresholds: create(RolloutAutomationThresholdsSchema, { maxNewErrors: 0 }),
        }),
      },
      false,
    );
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Max errors"), { target: { value: "" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
  });
});

describe("release channel scope synchronization", () => {
  const scopeWith = (id: bigint) =>
    create(ReleaseChannelScopeSchema, {
      siteIds: [id],
      buildingIds: [id],
      rackIds: [id],
      groupIds: [id],
      deviceIdentifiers: [`miner-${id}`],
    });
  const choose = (dimension: string, selection: string) => {
    fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${dimension} `) }));
    fireEvent.click(screen.getByRole("button", { name: selection }));
  };

  test.each([
    ["Sites", "siteIds", "Choose site 2", [2n]],
    ["Buildings", "buildingIds", "Choose building 2", [2n]],
    ["Racks", "rackIds", "Choose rack 2", [2n]],
    ["Groups", "groupIds", "Choose group 2", [2n]],
    ["Miners", "deviceIdentifiers", "Choose miner 2", ["miner-2"]],
  ] as const)(
    "keeps a local %s edit while accepting other scope dimensions from polling",
    async (label, field, selection, ids) => {
      const channel = { ...existingChannel(), scope: scopeWith(1n) };
      const { onSave, updateChannel, previewScope } = renderSettings(channel);
      choose(label, selection);
      const incoming = scopeWith(3n);
      updateChannel({ ...channel, scope: incoming }, false);
      const expected = create(ReleaseChannelScopeSchema, { ...incoming, [field]: [...ids] });
      await waitFor(() => expect(previewScope).toHaveBeenLastCalledWith(expected, channel.id, expect.any(AbortSignal)));
      expect(screen.getByTestId("save-channel")).toBeEnabled();
      await applyChannelSettings();
      expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ scope: expected }));
    },
  );

  test("preserves a local scope clear while accepting remote additions and removals", async () => {
    const channel = { ...existingChannel(), scope: scopeWith(1n) };
    const { onSave, updateChannel } = renderSettings(channel);
    choose("Sites", "Clear sites");
    const incoming = create(ReleaseChannelScopeSchema, { ...scopeWith(3n), rackIds: [], deviceIdentifiers: [] });
    updateChannel({ ...channel, scope: incoming }, false);
    await applyChannelSettings();
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        scope: create(ReleaseChannelScopeSchema, { ...incoming, siteIds: [] }),
      }),
    );
  });

  test("treats selector reordering as unchanged and accepts a later remote edit to that dimension", async () => {
    const channel = { ...existingChannel(), scope: create(ReleaseChannelScopeSchema, { siteIds: [1n, 2n] }) };
    const { onSave, updateChannel, previewScope } = renderSettings(channel);
    choose("Sites", "Choose sites 2 and 1");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    const incoming = scopeWith(3n);
    updateChannel({ ...channel, scope: incoming }, false);
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    await waitFor(() => expect(previewScope).toHaveBeenLastCalledWith(incoming, channel.id, expect.any(AbortSignal)));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    await applyChannelSettings();
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ scope: incoming }));
  });

  test("rebases a recovered scope against the acknowledged save before accepting later edits", async () => {
    const channel = { ...existingChannel(), scope: scopeWith(1n) };
    const write = deferredWrite();
    const onSave = vi
      .fn<(draft: ReleaseChannelDraft) => Promise<void>>()
      .mockReturnValueOnce(write.promise)
      .mockResolvedValue(undefined);
    const { updateChannel } = renderSettings(channel, onSave);
    choose("Sites", "Choose site 2");
    fireEvent.click(screen.getByTestId("save-channel"));
    fireEvent.click(within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" }));
    expect(screen.getByTestId("channel-settings")).toBeDisabled();
    updateChannel(channel, true);
    await act(async () => write.resolve());
    expect(onSave.mock.calls[0][0].scope).toEqual(
      create(ReleaseChannelScopeSchema, { ...channel.scope, siteIds: [2n] }),
    );
    openChannelSettings();
    choose("Buildings", "Choose building 2");
    const incoming = scopeWith(3n);
    updateChannel({ ...channel, scope: incoming }, false);
    await applyChannelSettings();
    expect(onSave.mock.calls[1][0].scope).toEqual(
      create(ReleaseChannelScopeSchema, { ...incoming, buildingIds: [2n] }),
    );
  });

  test("keeps a pending preview when polling changes only selector order", async () => {
    const channel = { ...existingChannel(), scope: create(ReleaseChannelScopeSchema, { siteIds: [1n, 2n] }) };
    const { previewScope, updateChannel } = renderSettings(channel);
    let finishPreview!: (preview: PreviewReleaseChannelScopeResponse) => void;
    previewScope.mockReturnValueOnce(
      new Promise((resolve) => {
        finishPreview = resolve;
      }),
    );
    await waitFor(() => expect(previewScope).toHaveBeenCalledOnce());
    updateChannel({ ...channel, scope: create(ReleaseChannelScopeSchema, { siteIds: [2n, 1n] }) }, false);
    await act(async () => finishPreview(create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 42 })));
    expect(screen.queryByTestId("scope-preview")).not.toBeInTheDocument();
    expect(previewScope).toHaveBeenCalledOnce();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  test("treats an absent remote scope as empty dimensions while retaining a local edit", async () => {
    const channel = { ...existingChannel(), scope: scopeWith(1n) };
    const { onSave, updateChannel } = renderSettings(channel);
    choose("Sites", "Choose site 2");
    updateChannel({ ...channel, scope: undefined }, false);
    await applyChannelSettings();
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        scope: create(ReleaseChannelScopeSchema, { siteIds: [2n] }),
      }),
    );
  });
});

describe("new release channel scope verification", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.useRealTimers();
    vi.mocked(useHasPermission).mockReturnValue(true);
  });

  const tick = (ms = 300) => act(async () => vi.advanceTimersByTimeAsync(ms));
  const chooseSite = (choice = "Choose site 2") => {
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: choice }));
  };
  const pendingPreview = deferred<PreviewReleaseChannelScopeResponse>;
  const cleanPreview = () => create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 1 });

  test("waits through debounce and pending resolution before creating a nonempty channel", async () => {
    const { onSave, previewScope } = renderManage(undefined);
    const pending = pendingPreview();
    previewScope.mockReturnValue(pending.promise);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New channel" } });
    const save = screen.getByTestId("save-channel");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    chooseSite();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Wait for the preview before creating the channel.");
    fireEvent.click(save);
    await tick(299);
    expect(previewScope).not.toHaveBeenCalled();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    await tick(1);
    expect(previewScope).toHaveBeenCalledOnce();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
    await act(async () => pending.resolve(cleanPreview()));
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await applyChannelSettings();
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ name: "New channel", scope: expect.objectContaining({ siteIds: [2n] }) }),
    );
  });

  test("keeps creation blocked after a failed preview and retries the same selection", async () => {
    const { onSave, previewScope } = renderManage(undefined);
    const retry = pendingPreview();
    previewScope.mockRejectedValueOnce(new Error("Preview unavailable")).mockReturnValueOnce(retry.promise);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New channel" } });
    chooseSite();
    await tick();
    expect(screen.getByRole("alert")).toHaveTextContent("Preview unavailable");
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("clear it to create an empty channel");
    const save = screen.getByTestId("save-channel");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Retry preview" }));
    expect(screen.queryByText("Preview unavailable")).not.toBeInTheDocument();
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Resolving 1 site");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    await tick();
    expect(previewScope).toHaveBeenCalledTimes(2);
    expect(previewScope.mock.calls[1][0]).toBe(previewScope.mock.calls[0][0]);
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    await act(async () => retry.resolve(cleanPreview()));
    expect(screen.getByTestId("save-channel")).toBeEnabled();
  });

  test("invalidates prior success and ignores late responses after changing or returning to a scope", async () => {
    const { onSave, previewScope } = renderManage(undefined);
    const oldPending = pendingPreview();
    const currentPending = pendingPreview();
    previewScope
      .mockResolvedValueOnce(cleanPreview())
      .mockReturnValueOnce(oldPending.promise)
      .mockReturnValueOnce(currentPending.promise);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New channel" } });
    chooseSite();
    await tick();
    const save = screen.getByTestId("save-channel");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    chooseSite("Choose sites 2 and 1");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    await tick();
    chooseSite();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Resolving 1 site");
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Last valid preview");
    await tick();
    await act(async () => oldPending.resolve(cleanPreview()));
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
    await act(async () => currentPending.resolve(cleanPreview()));
    expect(screen.getByTestId("save-channel")).toBeEnabled();
  });

  test("allows an empty selection without waiting for a pending preview", async () => {
    const { onSave, previewScope } = renderManage(undefined);
    const pending = pendingPreview();
    previewScope.mockReturnValue(pending.promise);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Empty channel" } });
    chooseSite();
    await tick();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    chooseSite("Clear sites");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await act(async () =>
      pending.resolve(
        create(PreviewReleaseChannelScopeResponseSchema, {
          conflictCount: 1,
          conflicts: [{ channelId: 2n, channelName: "Canary", minerCount: 1 }],
        }),
      ),
    );
    expect(screen.queryByTestId("scope-conflicts")).not.toBeInTheDocument();
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ scope: create(ReleaseChannelScopeSchema) }),
    );
  });

  test("creates an empty channel without catalog-read permission or preview requests", async () => {
    vi.mocked(useHasPermission).mockReturnValue(false);
    const { onSave, previewScope } = renderManage(undefined);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Empty channel" } });
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("You can save an empty channel.");
    expect(screen.queryByRole("button", { name: /^Sites / })).not.toBeInTheDocument();
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await tick();
    expect(previewScope).not.toHaveBeenCalled();
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ name: "Empty channel", scope: create(ReleaseChannelScopeSchema) }),
    );
  });

  test("allows existing-channel edits while preview is pending or failed", async () => {
    const { onSave, previewScope } = renderSettings(existingChannel());
    const pending = pendingPreview();
    previewScope.mockReturnValue(pending.promise);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });

    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await tick();
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await act(async () => pending.reject(new Error("Preview unavailable")));
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await applyChannelSettings();
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Renamed" }));
  });
});

describe("release channel saves during scope overlaps", () => {
  test("allows existing channel edits while preserving empty-name, unchanged and busy guards", async () => {
    const channel = existingChannel();
    let finishSaving!: () => void;
    const completion = new Promise<void>((resolve) => {
      finishSaving = resolve;
    });
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockReturnValue(completion);
    renderSettings(channel, onSave);
    await screen.findByTestId("scope-conflicts");
    const save = screen.getByTestId("save-channel");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "   " } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    expect(screen.getByTestId("scope-conflicts")).toHaveTextContent("Existing overlaps can remain");
    fireEvent.click(save);
    fireEvent.click(within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" }));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Renamed", scope: channel.scope }));
    expect(screen.getByTestId("channel-settings")).toBeDisabled();
    await act(async () => finishSaving());
    openChannelSettings();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Another edit" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
  });

  test("lets the server reject newly introduced overlaps and displays its error", async () => {
    const onSave = vi
      .fn<(draft: ReleaseChannelDraft) => Promise<void>>()
      .mockRejectedValue(new Error("Scope adds an overlap with Canary"));
    renderSettings(existingChannel(), onSave);
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 2" }));
    await screen.findByTestId("scope-conflicts");

    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await applyChannelSettings();
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Scope adds an overlap with Canary", status: "error" }),
    );
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ scope: expect.objectContaining({ siteIds: [2n] }) }),
    );
    expect(screen.getByLabelText("Name")).toHaveValue("Production");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
  });

  test("continues to block creating a channel whose scope overlaps", async () => {
    const { onSave } = renderManage(undefined);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New channel" } });
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 2" }));
    await screen.findByTestId("scope-conflicts");
    expect(screen.getByTestId("scope-conflicts")).toHaveTextContent(
      "Remove those miners from one of the channels before saving",
    );
    const save = screen.getByTestId("save-channel");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
  });
});

describe("release channel saves while refresh fails", () => {
  test("acknowledges a committed draft until reads recover without hiding subsequent edits", async () => {
    const channel = existingChannel();
    const { onSave, updateChannel } = renderSettings(channel, undefined, true);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved name" } });
    const save = screen.getByTestId("save-channel");
    await applyChannelSettings();
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Channel changes applied", status: "success" }),
    );
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledOnce();

    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "New unsaved description" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    // The server has the saved name, even though the last read still has
    // the original name. Reverting to that original value is a new edit.
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: channel.name } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved name" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();

    updateChannel({ ...channel, name: "Saved name" }, false);
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    updateChannel({ ...channel, name: "Another operator's name" }, false);
    expect(screen.getByLabelText("Name")).toHaveValue("Another operator's name");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    updateChannel({ ...channel, name: "Another operator's name" }, true);
    expect(screen.getByLabelText("Name")).toHaveValue("Another operator's name");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  test("accepts a later edit only after the acknowledged write completes", async () => {
    const channel = existingChannel();
    const write = deferredWrite();
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockReturnValue(write.promise);
    const { updateChannel } = renderSettings(channel, onSave);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Submitted name" } });
    fireEvent.click(screen.getByTestId("save-channel"));
    fireEvent.click(within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" }));
    expect(screen.getByTestId("channel-settings")).toBeDisabled();
    updateChannel(channel, true);
    await act(async () => write.resolve());
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Submitted name" }));
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Next unsaved name" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Submitted name" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  test("leaves a rejected write dirty and retryable", async () => {
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockRejectedValue(new Error("Save rejected"));
    renderSettings(existingChannel(), onSave, true);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved name" } });

    await applyChannelSettings();

    await waitFor(() => expect(pushToast).toHaveBeenCalledWith({ message: "Save rejected", status: "error" }));
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    expect(screen.getByLabelText("Name")).toHaveValue("Unsaved name");
  });
});

describe("assignment refresh after rollback", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });
  afterEach(() => vi.restoreAllMocks());

  test("retains a staged selection while blocking Apply until the rolled-back assignment refreshes", () => {
    const channel = assignedChannel("old-upload");
    const { updateChannel, onApply } = renderManage(channel, undefined, false, [replacementFile]);
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: /1\.4\.4/ }));
    const pending = {
      ...channel,
      modelGroups: channel.modelGroups.map((group) => ({ ...group, rollbackPending: true })),
    };
    updateChannel(pending, true);
    expect(screen.getByText("Rollback saved. Refreshing firmware assignment…")).toBeInTheDocument();
    expect(screen.queryByTestId("channel-firmware-select-Rig")).not.toBeInTheDocument();
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    expect(screen.getByTestId("view-miners-Rig")).toBeDisabled();
    expect(onApply).not.toHaveBeenCalled();
    const refreshed = {
      ...channel,
      modelGroups: channel.modelGroups.map((group) => ({
        ...group,
        assignmentGeneration: group.assignmentGeneration + 1n,
        firmwareChecksum: "b".repeat(64),
        firmwareFileId: "restored-upload",
        firmwareVersion: "1.4.2",
      })),
    };
    updateChannel(refreshed, false);
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("1.4.4");
    expect(screen.getByTestId("view-miners-Rig")).toBeEnabled();
    expect(screen.getByTestId("apply-firmware-changes")).not.toBeDisabled();
    expect(onApply).not.toHaveBeenCalled();
  });
});
