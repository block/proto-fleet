import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

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
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useHasPermission: () => true,
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
    channel,
    hasRefreshError,
    rollouts: [],
    firmwareFiles,
    minerNames: {},
    previewScope,
    listChannelMiners: vi.fn().mockResolvedValue([]),
    listRolloutDevices: vi.fn().mockResolvedValue([]),
    onSave,
    onApply: vi.fn().mockResolvedValue(undefined),
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

function deferredWrite() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

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
    const { onSave } = renderManage(channel);
    const coverage = screen.getByLabelText("Min sample coverage (%)");
    expect(coverage).toHaveValue("50");
    fireEvent.change(coverage, { target: { value: "0" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(coverage, { target: { value: "75" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[0][0].behavior.thresholds).toEqual(
      expect.objectContaining({ maxHashrateDropPercent: 10, minSampleCoveragePercent: 75 }),
    );
    fireEvent.change(coverage, { target: { value: "" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
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
      const { onSave } = renderManage(existingChannel());
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed channel" } });
      const chooseMethod = (name: string) => {
        fireEvent.click(screen.getByTestId("rollout-method"));
        fireEvent.click(screen.getByRole("option", { name: new RegExp(`^${name}`) }));
      };
      chooseMethod(method);
      fireEvent.change(screen.getByLabelText(label), { target: { value: invalidValue } });
      expect(screen.getByLabelText(label)).toHaveAttribute("aria-invalid", "true");
      expect(screen.getByText("Enter at least 1 miner.")).toBeVisible();
      const save = screen.getByTestId("save-channel");
      expect(save).toBeDisabled();
      fireEvent.click(save);
      expect(onSave).not.toHaveBeenCalled();
      chooseMethod(method === "Multiple batches" ? "Pilot batch, then remaining" : "Multiple batches");
      expect(save).toBeEnabled();
      chooseMethod("Single batch");
      expect(save).toBeEnabled();
      chooseMethod(method);
      expect(save).toBeDisabled();
      fireEvent.change(screen.getByLabelText(label), { target: { value: "1" } });
      expect(screen.getByLabelText(label)).not.toHaveAttribute("aria-invalid");
      expect(save).toBeEnabled();
      await act(async () => fireEvent.click(save));
      expect(onSave).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ behavior: expect.objectContaining({ [field]: 1 }) }),
      );
    },
  );

  test("allows firmware Apply to use saved settings when an unsaved active size is invalid", async () => {
    const { onSave, onApply } = renderManage(assignedChannel(), undefined, false, [replacementFile]);
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Multiple batches/ }));
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "0" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: /1\.4\.4/ }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("Pacing: single batch.");
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("Unsaved channel changes");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
    expect(onApply).toHaveBeenCalledOnce();
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
      channel: originalChannel,
      rollouts: [gatedRigRollout],
      firmwareFiles: [],
      minerNames: {},
      previewScope: vi.fn().mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema)),
      listChannelMiners: vi.fn().mockResolvedValue([]),
      listRolloutDevices: vi.fn().mockResolvedValue([]),
      onSave: vi.fn().mockResolvedValue(undefined),
      onApply: vi.fn().mockResolvedValue(undefined),
    };
    props.listRolloutDevices.mockImplementationOnce(
      (_id: bigint, signal: AbortSignal) =>
        new Promise<never[]>((resolve) => signal.addEventListener("abort", () => resolve([]), { once: true })),
    );
    const { rerender } = render(<ReleaseChannelManageView {...props} />);
    expect(screen.getByTestId("model-group-Rig")).toHaveTextContent("Review needed");
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
    expect(screen.getByTestId("model-group-rollout-progress-Rig")).toHaveTextContent("Updating to 2.0.0");
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
    renderManage(channel);
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
    expect(screen.getByText(/1 firmware change pending/)).toBeInTheDocument();
    expect(controls).not.toHaveTextContent(aggregatePlan);
  });

  test("keeps scope counts visible without treating one model or a draft preview as rollout targets", async () => {
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
    const { previewScope } = renderManage(channel);
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
    await waitFor(() => expect(screen.getByTestId("scope-preview")).toHaveTextContent("covers 60 miners"));
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
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Multiple batches/ }));
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: batchSize } });
  };

  test("confirms acknowledged pacing after a failed refresh without treating newer or rejected drafts as saved", async () => {
    const channel = assignedChannel();
    const write = deferredWrite();
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockReturnValueOnce(write.promise);
    const { updateChannel, onApply } = renderManage(channel, onSave, false, [replacementFile]);
    chooseBatches("3");
    stageReplacement();
    const save = screen.getByTestId("save-channel");
    const apply = screen.getByTestId("apply-firmware-changes");
    fireEvent.click(save);
    expect(apply).toBeDisabled();
    fireEvent.click(apply);
    expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "8" } });
    updateChannel(channel, true);
    await act(async () => write.resolve());
    expect(onSave.mock.calls[0][0].behavior.batchSize).toBe(3);
    expect(save).toBeEnabled();
    expect(apply).toBeEnabled();
    fireEvent.click(apply);
    const dialog = screen.getByTestId("apply-firmware-dialog");
    expect(dialog).toHaveTextContent("Pacing: batches of 3, back to back.");
    expect(dialog).not.toHaveTextContent("batches of 8");
    expect(dialog).toHaveTextContent("Unsaved channel changes");

    onSave.mockRejectedValueOnce(new Error("Newer save rejected"));
    await act(async () => fireEvent.click(save));
    expect(dialog).toHaveTextContent("Pacing: batches of 3, back to back.");
    expect(screen.getByLabelText("Batch size (miners)")).toHaveValue("8");
    expect(save).toBeEnabled();

    const refreshed = {
      ...channel,
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.BATCHED,
        order: RolloutOrder.LEAST_EFFICIENT_FIRST,
        batchSize: 5,
      }),
    };
    updateChannel(refreshed, false);
    expect(dialog).toHaveTextContent("Pacing: batches of 5, back to back.");
    updateChannel(refreshed, true);
    expect(dialog).toHaveTextContent("Pacing: batches of 5, back to back.");
    expect(dialog).not.toHaveTextContent("batches of 3");
  });

  test("blocks an already-open confirmation during save and permits retry after save rejection", async () => {
    const channel = assignedChannel();
    const write = deferredWrite();
    const onSave = vi
      .fn<(draft: ReleaseChannelDraft) => Promise<void>>()
      .mockResolvedValue(undefined)
      .mockReturnValueOnce(write.promise);
    const { onApply } = renderManage(channel, onSave, true, [replacementFile]);
    stageReplacement();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    chooseBatches("7");
    const save = screen.getByTestId("save-channel");
    const start = screen.getByRole("button", { name: "Start update" });
    fireEvent.click(save);
    expect(start).toBeDisabled();
    fireEvent.click(start);
    expect(onApply).not.toHaveBeenCalled();
    await act(async () => write.reject(new Error("Settings rejected")));
    expect(start).toBeEnabled();
    expect(save).toBeEnabled();
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("Pacing: single batch.");
    expect(pushToast).toHaveBeenCalledWith({ message: "Settings rejected", status: "error" });

    await act(async () => fireEvent.click(save));
    expect(onSave).toHaveBeenCalledTimes(2);
    expect(save).toBeDisabled();
    expect(start).toBeEnabled();
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("Pacing: batches of 7, back to back.");
    await act(async () => fireEvent.click(start));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "replacement" },
    ]);
  });

  test.each(["success", "failure"])("blocks settings saves until an in-flight apply ends in %s", async (outcome) => {
    const { onApply, onSave } = renderManage(assignedChannel(), undefined, false, [replacementFile]);
    const write = deferredWrite();
    onApply.mockReturnValueOnce(write.promise);
    stageReplacement();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved name" } });
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const save = screen.getByTestId("save-channel");
    const start = screen.getByRole("button", { name: "Start update" });
    fireEvent.click(start);
    expect(save).toBeDisabled();
    expect(start).toBeDisabled();
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    fireEvent.click(save);
    fireEvent.click(start);
    expect(onSave).not.toHaveBeenCalled();
    expect(onApply).toHaveBeenCalledOnce();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Newer unsaved name" } });
    await act(async () => {
      if (outcome === "success") write.resolve();
      else write.reject(new Error("Apply rejected"));
    });
    expect(save).toBeEnabled();
    if (outcome === "failure") {
      expect(start).toBeEnabled();
      expect(screen.getByText(/1 firmware change pending/)).toBeInTheDocument();
      await act(async () => fireEvent.click(start));
      expect(onApply).toHaveBeenCalledTimes(2);
      expect(onApply.mock.calls[1]).toEqual(onApply.mock.calls[0]);
    }
    await act(async () => fireEvent.click(save));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Newer unsaved name" }));
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
    expect(screen.getByText(/1 firmware change pending/)).toBeInTheDocument();

    updateChannel(channel, true);
    expect(picker).toHaveTextContent("No firmware");
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("no firmware");
    fireEvent.click(screen.getByRole("button", { name: "Clear assignments" }));
    await waitFor(() =>
      expect(onApply).toHaveBeenCalledExactlyOnceWith(channel.id, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: "" },
      ]),
    );
    await waitFor(() => expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument());
  });

  test("discarding a staged clear restores the unavailable assignment without changing channel edits", () => {
    renderManage(assignedChannel());
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved name" } });
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    fireEvent.click(picker);
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(picker).toHaveTextContent("1.4.3");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Unsaved name");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
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
      expect(screen.getByText(/2 firmware changes pending/)).toBeInTheDocument();
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("Firmware details unavailable");
      expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent(
        "Choose valid firmware for every changed model or discard the pending changes.",
      );
      expect(apply).toBeDisabled();
      expect(start).toBeDisabled();
      fireEvent.click(start);
      expect(onApply).not.toHaveBeenCalled();
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Independent settings change" } });
      await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
      expect(onSave).toHaveBeenCalledOnce();
      expect(start).toBeDisabled();

      fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
      fireEvent.click(screen.getByRole("option", { name: /1.4.5/ }));
      expect(apply).toBeEnabled();
      expect(start).toBeEnabled();
      await act(async () => fireEvent.click(start));
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
      expect(screen.getByText(/2 firmware changes pending/)).toBeInTheDocument();
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
    expect(screen.getByText(/2 firmware changes pending/)).toBeInTheDocument();
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
    expect(screen.getByText(/1 firmware change pending/)).toBeInTheDocument();
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
      expect(screen.getByText(/2 firmware changes pending/)).toBeInTheDocument();
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
      expect(screen.getByText(/2 firmware changes pending/)).toBeInTheDocument();
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
      expect(screen.getByText(/2 firmware changes pending/)).toBeInTheDocument();
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
    const { onSave, updateChannel } = renderManage(channel);
    const save = screen.getByTestId("save-channel");
    expect(save).toBeDisabled();
    change();
    expect(save).toBeEnabled();
    await act(async () => fireEvent.click(save));
    expect(onSave).toHaveBeenCalledOnce();
    // The form keeps values for switching back; serialization strips them.
    expect(onSave.mock.calls[0][0].behavior.thresholds).toBeUndefined();
    updateChannel({ ...channel, behavior: saved() }, false);
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Max miners offline at once (0 for no limit)"), { target: { value: "5" } });
    expect(save).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Max miners offline at once (0 for no limit)"), { target: { value: "4" } });
    expect(save).toBeDisabled();
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
    const { onSave, updateChannel } = renderManage(channel);
    const save = screen.getByTestId("save-channel");
    fireEvent.click(screen.getByLabelText("Review after each batch"));
    await act(async () => fireEvent.click(save));
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
    expect(save).toBeDisabled();
    fireEvent.click(screen.getByLabelText("Review after each batch"));
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Pilot batch/ }));
    fireEvent.change(screen.getByLabelText("Pilot batch size (miners)"), { target: { value: "2" } });
    await act(async () => fireEvent.click(save));
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
    expect(save).toBeDisabled();
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
    const { updateChannel } = renderManage(channel);
    const save = screen.getByTestId("save-channel");
    fireEvent.change(screen.getByLabelText("Max hashrate drop (%)"), { target: { value: "" } });
    await act(async () => fireEvent.click(save));
    updateChannel(
      { ...channel, behavior: create(RolloutBehaviorSchema, { ...behavior, thresholds: undefined }) },
      false,
    );
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Max errors"), { target: { value: "0" } });
    expect(save).toBeEnabled();
    await act(async () => fireEvent.click(save));
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
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Max errors"), { target: { value: "" } });
    expect(save).toBeEnabled();
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
      const { onSave, updateChannel, previewScope } = renderManage(channel);
      choose(label, selection);
      const incoming = scopeWith(3n);
      updateChannel({ ...channel, scope: incoming }, false);
      const expected = create(ReleaseChannelScopeSchema, { ...incoming, [field]: [...ids] });
      await waitFor(() => expect(previewScope).toHaveBeenLastCalledWith(expected, channel.id));
      expect(screen.getByTestId("save-channel")).toBeEnabled();
      await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
      expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ scope: expected }));
    },
  );

  test("preserves a local scope clear while accepting remote additions and removals", async () => {
    const channel = { ...existingChannel(), scope: scopeWith(1n) };
    const { onSave, updateChannel } = renderManage(channel);
    choose("Sites", "Clear sites");
    const incoming = create(ReleaseChannelScopeSchema, { ...scopeWith(3n), rackIds: [], deviceIdentifiers: [] });
    updateChannel({ ...channel, scope: incoming }, false);
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        scope: create(ReleaseChannelScopeSchema, { ...incoming, siteIds: [] }),
      }),
    );
  });

  test("treats selector reordering as unchanged and accepts a later remote edit to that dimension", async () => {
    const channel = { ...existingChannel(), scope: create(ReleaseChannelScopeSchema, { siteIds: [1n, 2n] }) };
    const { onSave, updateChannel, previewScope } = renderManage(channel);
    choose("Sites", "Choose sites 2 and 1");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    const incoming = scopeWith(3n);
    updateChannel({ ...channel, scope: incoming }, false);
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    await waitFor(() => expect(previewScope).toHaveBeenLastCalledWith(incoming, channel.id));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ scope: incoming }));
  });

  test("rebases a recovered scope against the acknowledged save while retaining edits made during Save", async () => {
    const channel = { ...existingChannel(), scope: scopeWith(1n) };
    const write = deferredWrite();
    const onSave = vi
      .fn<(draft: ReleaseChannelDraft) => Promise<void>>()
      .mockReturnValueOnce(write.promise)
      .mockResolvedValue(undefined);
    const { updateChannel } = renderManage(channel, onSave);
    choose("Sites", "Choose site 2");
    fireEvent.click(screen.getByTestId("save-channel"));
    choose("Buildings", "Choose building 2");
    updateChannel(channel, true);
    await act(async () => write.resolve());
    expect(onSave.mock.calls[0][0].scope).toEqual(
      create(ReleaseChannelScopeSchema, { ...channel.scope, siteIds: [2n] }),
    );
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    const incoming = scopeWith(3n);
    updateChannel({ ...channel, scope: incoming }, false);
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave.mock.calls[1][0].scope).toEqual(
      create(ReleaseChannelScopeSchema, { ...incoming, buildingIds: [2n] }),
    );
  });

  test("keeps a pending preview when polling changes only selector order", async () => {
    const channel = { ...existingChannel(), scope: create(ReleaseChannelScopeSchema, { siteIds: [1n, 2n] }) };
    const { previewScope, updateChannel } = renderManage(channel);
    let finishPreview!: (preview: PreviewReleaseChannelScopeResponse) => void;
    previewScope.mockReturnValueOnce(
      new Promise((resolve) => {
        finishPreview = resolve;
      }),
    );
    await waitFor(() => expect(previewScope).toHaveBeenCalledOnce());
    updateChannel({ ...channel, scope: create(ReleaseChannelScopeSchema, { siteIds: [2n, 1n] }) }, false);
    await act(async () => finishPreview(create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 42 })));
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("covers 42 miners");
    expect(previewScope).toHaveBeenCalledOnce();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  test("treats an absent remote scope as empty dimensions while retaining a local edit", async () => {
    const channel = { ...existingChannel(), scope: scopeWith(1n) };
    const { onSave, updateChannel } = renderManage(channel);
    choose("Sites", "Choose site 2");
    updateChannel({ ...channel, scope: undefined }, false);
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        scope: create(ReleaseChannelScopeSchema, { siteIds: [2n] }),
      }),
    );
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
    renderManage(channel, onSave);
    await screen.findByTestId("scope-conflicts");
    const save = screen.getByTestId("save-channel");
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "   " } });
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    expect(save).toBeEnabled();
    expect(screen.getByTestId("scope-conflicts")).toHaveTextContent("Existing overlaps can remain");
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Renamed", scope: channel.scope }));
    expect(save).toBeDisabled();
    await act(async () => finishSaving());
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Another edit" } });
    expect(save).toBeEnabled();
  });

  test("lets the server reject newly introduced overlaps and displays its error", async () => {
    const onSave = vi
      .fn<(draft: ReleaseChannelDraft) => Promise<void>>()
      .mockRejectedValue(new Error("Scope adds an overlap with Canary"));
    renderManage(existingChannel(), onSave);
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 2" }));
    await screen.findByTestId("scope-conflicts");
    const save = screen.getByTestId("save-channel");
    expect(save).toBeEnabled();
    fireEvent.click(save);
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Scope adds an overlap with Canary", status: "error" }),
    );
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ scope: expect.objectContaining({ siteIds: [2n] }) }),
    );
    expect(screen.getByLabelText("Name")).toHaveValue("Production");
    expect(save).toBeEnabled();
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
    expect(save).toBeDisabled();
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
  });
});

describe("release channel saves while refresh fails", () => {
  test("acknowledges a committed draft until reads recover without hiding subsequent edits", async () => {
    const channel = existingChannel();
    const { onSave, updateChannel } = renderManage(channel, undefined, true);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved name" } });
    const save = screen.getByTestId("save-channel");
    fireEvent.click(save);
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Release channel saved", status: "success" }),
    );
    expect(save).toBeDisabled();
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledOnce();

    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "New unsaved description" } });
    expect(save).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "" } });
    expect(save).toBeDisabled();
    // The server has the saved name, even though the last read still has
    // the original name. Reverting to that original value is a new edit.
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: channel.name } });
    expect(save).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved name" } });
    expect(save).toBeDisabled();

    updateChannel({ ...channel, name: "Saved name" }, false);
    expect(save).toBeDisabled();
    updateChannel({ ...channel, name: "Another operator's name" }, false);
    expect(screen.getByLabelText("Name")).toHaveValue("Another operator's name");
    expect(save).toBeDisabled();
    updateChannel({ ...channel, name: "Another operator's name" }, true);
    expect(screen.getByLabelText("Name")).toHaveValue("Another operator's name");
    expect(save).toBeDisabled();
  });

  test("does not acknowledge local edits made while the successful save was pending", async () => {
    const channel = existingChannel();
    let finishSaving!: () => void;
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockReturnValue(
      new Promise<void>((resolve) => {
        finishSaving = resolve;
      }),
    );
    const { updateChannel } = renderManage(channel, onSave);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Submitted name" } });
    const save = screen.getByTestId("save-channel");
    fireEvent.click(save);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Next unsaved name" } });
    updateChannel(channel, true);
    await act(async () => finishSaving());

    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Submitted name" }));
    expect(screen.getByLabelText("Name")).toHaveValue("Next unsaved name");
    expect(save).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Submitted name" } });
    expect(save).toBeDisabled();
  });

  test("leaves a rejected write dirty and retryable", async () => {
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockRejectedValue(new Error("Save rejected"));
    renderManage(existingChannel(), onSave, true);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved name" } });
    const save = screen.getByTestId("save-channel");
    fireEvent.click(save);

    await waitFor(() => expect(pushToast).toHaveBeenCalledWith({ message: "Save rejected", status: "error" }));
    expect(save).toBeEnabled();
    expect(screen.getByLabelText("Name")).toHaveValue("Unsaved name");
  });
});
