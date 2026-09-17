import type { ComponentProps } from "react";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  PreviewReleaseChannelScopeResponseSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
  type Rollout,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { AssignmentDraft, ChannelView } from "@/protoFleet/api/useReleaseChannels";

vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: () => null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

const file = (id: string, model = "Rig"): FirmwareFileInfo => ({
  id,
  filename: `${id}.swu`,
  firmware_version: `${id}-catalog`,
  target_manufacturer: "Proto",
  target_model: model,
  uploaded_at: "2026-09-17T00:00:00Z",
  size: 100,
});
const firmwareFiles = [
  file("old"),
  file("next"),
  file("later"),
  file("second-old", "Other"),
  file("second-next", "Other"),
];
const group = (model = "Rig", id = "old") =>
  create(ReleaseChannelModelGroupSchema, {
    manufacturer: "proto",
    model,
    firmwareFileId: id,
    firmwareChecksum: `${id}-checksum`,
    firmwareVersion: `${id}-saved`,
    firmwareTargetManufacturer: "Proto",
    firmwareTargetModel: model,
    firmwareAvailable: true,
    assignmentGeneration: 1n,
    minerCount: 1,
    onTargetCount: 1,
  });
const channelFor = (method = RolloutMethod.ALL_AT_ONCE): ChannelView => ({
  ...create(ReleaseChannelSchema, { id: 1n, name: "Production", behavior: { method } }),
  modelGroups: [group()],
});
const started = (model = "Rig", version = "next-server") =>
  create(RolloutSchema, {
    channelId: 1n,
    manufacturer: "Proto",
    model,
    firmwareVersion: version,
  });

function deferred() {
  let resolve!: (result: Rollout[]) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<Rollout[]>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function renderManage(channel = channelFor(), hasRefreshError = false) {
  const onApply = vi
    .fn<(channelId: bigint, assignments: AssignmentDraft[]) => Promise<Rollout[]>>()
    .mockResolvedValue([started()]);
  const onSave = vi.fn().mockResolvedValue(undefined);
  let props: ComponentProps<typeof ReleaseChannelManageView> = {
    channel,
    hasRefreshError,
    rollouts: [],
    firmwareFiles,
    minerNames: {},
    previewScope: vi.fn().mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema)),
    listChannelMiners: vi.fn().mockResolvedValue([]),
    listRolloutDevices: vi.fn().mockResolvedValue([]),
    onSave,
    onApply,
  };
  const { rerender } = render(<ReleaseChannelManageView {...props} />);
  return {
    onApply,
    onSave,
    update: (patch: Partial<typeof props>) => {
      props = { ...props, ...patch };
      rerender(<ReleaseChannelManageView {...props} />);
    },
  };
}

function chooseFile(id: string, model = "Rig") {
  fireEvent.click(screen.getByTestId(`channel-firmware-select-${model}`));
  const list = screen.getByRole("listbox", { name: `Firmware for proto ${model} options` });
  fireEvent.click(within(list).getByRole("option", { name: id === "" ? "No firmware" : new RegExp(`^${id}-`) }));
}
function chooseMethod(label: string) {
  fireEvent.click(screen.getByTestId("rollout-method"));
  fireEvent.click(screen.getByRole("option", { name: new RegExp(`^${label}`) }));
}
async function startApply() {
  fireEvent.click(screen.getByTestId("apply-firmware-changes"));
  await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
});
afterEach(() => vi.restoreAllMocks());

describe("firmware Apply with saved delegated behavior", () => {
  it("requires saving a supported method before starting firmware, including after a failed refresh", async () => {
    const { onApply, onSave } = renderManage(channelFor(RolloutMethod.DELEGATED), true);
    chooseFile("next");
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    expect(screen.getByTestId("delegated-apply-unavailable")).toHaveTextContent(
      "Choose and save another update method",
    );
    chooseMethod("Single batch");
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(onApply).not.toHaveBeenCalled();
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledOnce();
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    expect(screen.queryByTestId("delegated-apply-unavailable")).not.toBeInTheDocument();
    await startApply();
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "next" },
    ]);
  });

  it("blocks a mixed set and clear but allows clearing all delegated assignments, including unavailable files", async () => {
    const channel = channelFor(RolloutMethod.DELEGATED);
    channel.modelGroups.push({ ...group("Other", "second-old"), firmwareFileId: "", firmwareAvailable: false });
    const { onApply } = renderManage(channel);
    chooseFile("next");
    chooseFile("", "Other");
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    chooseFile("");
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    expect(screen.queryByTestId("delegated-apply-unavailable")).not.toBeInTheDocument();
    await startApply();
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "" },
      { manufacturer: "Proto", model: "Other", firmwareFileId: "" },
    ]);
  });

  it("disables an open confirmation if the saved method becomes delegated", async () => {
    const channel = channelFor();
    const { update, onApply } = renderManage(channel);
    chooseFile("next");
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    update({ channel: { ...channel, behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED }) } });
    expect(screen.getByRole("button", { name: "Start update" })).toBeDisabled();
    expect(screen.getByTestId("apply-firmware-dialog")).toHaveTextContent("Choose and save another update method");
    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
    expect(onApply).not.toHaveBeenCalled();
    chooseMethod("Single batch");
    expect(screen.getByRole("button", { name: "Start update" })).toBeDisabled();
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(screen.getByRole("button", { name: "Start update" })).toBeEnabled();
  });
});

describe("acknowledged firmware assignments before read recovery", () => {
  it("withholds stale miner details for an acknowledged assignment until reads recover", async () => {
    const channel = channelFor();
    channel.modelGroups.push(group("Other", "second-old"));
    const { update } = renderManage(channel, true);
    chooseFile("next");
    fireEvent.click(screen.getByTestId("view-miners-Rig"));
    expect(screen.getByText("proto Rig miners")).toBeInTheDocument();
    await startApply();
    expect(screen.queryByText("proto Rig miners")).not.toBeInTheDocument();
    expect(screen.getByTestId("view-miners-Rig")).toBeDisabled();
    expect(screen.getByTestId("view-miners-Other")).toBeEnabled();
    update({
      channel: { ...channel, modelGroups: [group("Rig", "next"), group("Other", "second-old")] },
      hasRefreshError: false,
    });
    expect(screen.getByTestId("view-miners-Rig")).toBeEnabled();
    expect(screen.getByText("proto Rig miners")).toBeInTheDocument();
  });

  it("retains the returned assignment version without duplicate Apply until a successful new snapshot", async () => {
    const channel = channelFor();
    const { update, onApply } = renderManage(channel, true);
    chooseFile("next");
    await startApply();
    const picker = screen.getByTestId("channel-firmware-select-Rig");
    expect(picker).toHaveTextContent("next-server");
    expect(screen.getByText("Refreshing update status")).toBeInTheDocument();
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    update({
      channel: { ...channel },
      firmwareFiles: firmwareFiles.map((f) => ({ ...f, firmware_version: `${f.id}-edited` })),
    });
    expect(picker).toHaveTextContent("next-server");
    chooseFile("next");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(onApply).toHaveBeenCalledOnce();
    update({
      channel: { ...channel, modelGroups: [{ ...group("Rig", "next"), firmwareVersion: "next-authoritative" }] },
      hasRefreshError: false,
    });
    expect(picker).toHaveTextContent("next-authoritative");
    expect(screen.queryByText("Refreshing update status")).not.toBeInTheDocument();
  });

  it("marks missing response metadata pending and retains it until a distinct successful read", async () => {
    const channel = channelFor();
    const { update, onApply } = renderManage(channel);
    onApply.mockResolvedValue([]);
    chooseFile("next");
    await startApply();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("next-catalog (details pending)");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    update({ channel });
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("details pending");
    update({ channel: { ...channel } });
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("old-saved");
    expect(screen.queryByText("Refreshing update status")).not.toBeInTheDocument();
  });

  it("keeps an acknowledged clear separate from the stale assignment checksum", async () => {
    const { onApply } = renderManage(channelFor(), true);
    onApply.mockResolvedValue([]);
    chooseFile("");
    await startApply();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
    chooseFile("");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    chooseFile("next");
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
  });

  it.each(["later", "old"])(
    "preserves a newer %s selection made during Apply and Discard restores the acknowledged selection",
    async (newer) => {
      const channel = channelFor();
      const { onApply, update } = renderManage(channel);
      const write = deferred();
      onApply.mockReturnValueOnce(write.promise);
      chooseFile("next");
      await startApply();
      chooseFile(newer);
      update({ channel: { ...channel }, hasRefreshError: true });
      await act(async () => write.resolve([started()]));
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent(`${newer}-catalog`);
      expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
      fireEvent.click(screen.getByRole("button", { name: "Discard" }));
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("next-server");
      expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    },
  );

  it("preserves unavailable newer choices and their recovery guard after Apply succeeds", async () => {
    const { onApply, update } = renderManage(channelFor(), true);
    const write = deferred();
    onApply.mockReturnValueOnce(write.promise);
    chooseFile("next");
    await startApply();
    chooseFile("later");
    update({ firmwareFiles: firmwareFiles.filter((f) => f.id !== "later") });
    await act(async () => write.resolve([started()]));
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("Firmware details unavailable");
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    expect(screen.getByText(/Selected firmware is unavailable for this model/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("next-server");
  });

  it("retains acknowledgements from separate successful Apply calls while reads keep failing", async () => {
    const channel = channelFor();
    channel.modelGroups.push(group("Other", "second-old"));
    const { onApply } = renderManage(channel, true);
    chooseFile("next");
    await startApply();
    chooseFile("second-next", "Other");
    onApply.mockResolvedValueOnce([started("Other", "second-server")]);
    await startApply();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("next-server");
    expect(screen.getByTestId("channel-firmware-select-Other")).toHaveTextContent("second-server");
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(onApply).toHaveBeenCalledTimes(2);
  });

  it("retains rejected changes for retry without acknowledging them", async () => {
    const { onApply } = renderManage(channelFor(), true);
    onApply.mockRejectedValueOnce(new Error("Apply rejected"));
    chooseFile("next");
    await startApply();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("next-catalog");
    expect(screen.queryByText("Refreshing update status")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start update" })).toBeEnabled();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));
    expect(onApply).toHaveBeenCalledTimes(2);
  });

  it("does not resurrect an earlier acknowledgement retired by a successful poll during another Apply", async () => {
    const channel = channelFor();
    channel.modelGroups.push(group("Other", "second-old"));
    const { onApply, update } = renderManage(channel, true);
    chooseFile("next");
    await startApply();
    const write = deferred();
    onApply.mockReturnValueOnce(write.promise);
    chooseFile("second-next", "Other");
    await startApply();
    const refreshed = { ...channel, modelGroups: [group("Rig", "later"), group("Other", "second-old")] };
    update({ channel: refreshed, hasRefreshError: false });
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("later-saved");
    update({ hasRefreshError: true });
    await act(async () => write.resolve([started("Other", "second-server")]));
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("later-saved");
    expect(screen.getByTestId("channel-firmware-select-Other")).toHaveTextContent("second-server");
    expect(screen.getAllByText("Refreshing update status")).toHaveLength(1);
  });
});
