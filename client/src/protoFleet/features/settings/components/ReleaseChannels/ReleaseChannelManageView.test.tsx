import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { defaultBehavior } from "./behaviorUtils";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  PreviewReleaseChannelScopeResponseSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
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
    open ? <button onClick={() => onSave({ siteIds: ["2"] })}>Choose site 2</button> : null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
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
    const { onSave, onApply } = renderManage(assignedChannel());
    fireEvent.click(screen.getByTestId("rollout-method"));
    fireEvent.click(screen.getByRole("option", { name: /^Multiple batches/ }));
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "0" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("Pacing: single batch.");
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("Unsaved channel changes");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
    expect(onApply).toHaveBeenCalledOnce();
    expect(onSave).not.toHaveBeenCalled();
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

  const stageClear = () => {
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
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
    const { updateChannel, onApply } = renderManage(channel, onSave);
    chooseBatches("3");
    stageClear();
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
    const { onApply } = renderManage(channel, onSave, true);
    stageClear();
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
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [{ manufacturer: "Proto", model: "Rig", firmwareFileId: "" }]);
  });

  test.each(["success", "failure"])("blocks settings saves until an in-flight apply ends in %s", async (outcome) => {
    const { onApply, onSave } = renderManage(assignedChannel());
    const write = deferredWrite();
    onApply.mockReturnValueOnce(write.promise);
    stageClear();
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
    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
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
  ] as const)("preserves the assigned version when uploaded file metadata is %s", (_, files) => {
    renderManage(assignedChannel("replacement"), undefined, false, [...files]);
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    expect(picker).toHaveTextContent("1.4.3");
    expect(picker).not.toHaveTextContent("No firmware");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    fireEvent.click(picker);
    expect(screen.getByRole("option", { name: "No firmware" })).toHaveAttribute("aria-selected", "false");
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
      { manufacturer: " PROTO ", model: " rig ", firmwareFileId: "replacement" },
    ]);
  });

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
      fireEvent.click(screen.getByRole("button", { name: "Start update" }));
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
    await waitFor(() => expect(listChannelMiners).toHaveBeenCalledExactlyOnceWith(1n, " PROTO ", " rig "));
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
    await waitFor(() => expect(save).toBeEnabled());
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
    expect(save).toBeEnabled();
    updateChannel({ ...channel, name: "Another operator's name" }, true);
    expect(save).toBeEnabled();
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
