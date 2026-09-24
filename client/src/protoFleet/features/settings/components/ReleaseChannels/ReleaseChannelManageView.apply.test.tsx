import type { ComponentProps } from "react";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { closeChannelSettings, deferred, manageViewProps, openChannelSettings } from "./__tests__/helpers";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
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

function renderManage(channel = channelFor(), hasRefreshError = false) {
  const onApply = vi
    .fn<(channelId: bigint, assignments: AssignmentDraft[]) => Promise<Rollout[]>>()
    .mockResolvedValue([started()]);
  const onSave = vi.fn().mockResolvedValue(undefined);
  let props: ComponentProps<typeof ReleaseChannelManageView> = {
    ...manageViewProps(),
    channel,
    hasRefreshError,
    firmwareFiles,
    onSave,
    onApply,
    onDelete: vi.fn(),
    onShowHistory: vi.fn(),
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
  openChannelSettings();
  fireEvent.click(screen.getByTestId("rollout-method"));
  fireEvent.click(screen.getByRole("option", { name: new RegExp(`^${label}`) }));
}
async function startApply(button = "Start update") {
  closeChannelSettings();
  fireEvent.click(screen.getByTestId("apply-firmware-changes"));
  await act(async () =>
    fireEvent.click(within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: button })),
  );
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
});
afterEach(() => vi.restoreAllMocks());

describe("pending firmware header actions", () => {
  it("replaces channel actions while staging and restores them after discarding", () => {
    const { onApply, onSave } = renderManage();
    for (const name of ["Channel settings", "History"]) {
      expect(screen.getByRole("button", { name })).toBeVisible();
    }
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Discard" })).not.toBeInTheDocument();

    chooseFile("next");
    expect(screen.getByTestId("channel-settings")).toBeEnabled();
    for (const name of ["History"]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    expect(screen.getByRole("button", { name: "Discard" })).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    for (const name of ["Channel settings", "History"]) {
      expect(screen.getByRole("button", { name })).toBeVisible();
    }
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Discard" })).not.toBeInTheDocument();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("old-saved");
    expect(onApply).not.toHaveBeenCalled();
    expect(onSave).not.toHaveBeenCalled();
  });
  it("offers the same Apply and Discard actions for settings-only changes", () => {
    const { onApply, onSave } = renderManage();
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Draft name" } });
    closeChannelSettings();
    expect(screen.queryByText(/changes? pending/i)).not.toBeInTheDocument();
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("1");
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    expect(screen.getByTestId("channel-settings")).toBeEnabled();
    expect(screen.queryByRole("button", { name: "History" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("delete-channel")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "History" })).toBeVisible();
    expect(screen.queryByTestId("delete-channel")).not.toBeInTheDocument();
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Production");
    expect(onSave).not.toHaveBeenCalled();
    expect(onApply).not.toHaveBeenCalled();
  });

  it("counts settings and model changes in Apply, updates on revert, and keeps pending text in the preview", async () => {
    const channel = channelFor();
    channel.modelGroups.push(group("Other", "second-old"));
    renderManage(channel);
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Canary" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Draft description" } });
    closeChannelSettings();
    chooseFile("next");
    chooseFile("second-next", "Other");

    let apply = screen.getByRole("button", { name: "Apply changes (4)" });
    expect(within(apply).getByTestId("pending-change-count")).toHaveTextContent("4");
    expect(screen.queryByText(/changes? pending/i)).not.toBeInTheDocument();
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Production" } });
    closeChannelSettings();
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("3");
    chooseFile("second-old", "Other");
    apply = screen.getByRole("button", { name: "Apply changes (2)" });
    expect(within(apply).getByTestId("pending-change-count")).toHaveTextContent("2");
    fireEvent.click(apply);

    const dialog = screen.getByTestId("apply-firmware-dialog");
    expect(within(dialog).getByText("2 changes pending")).toBeInTheDocument();
    expect(screen.getAllByText(/changes? pending/i)).toHaveLength(1);
    expect(dialog).toHaveTextContent("Draft description");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText(/changes? pending/i)).not.toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    expect(screen.queryByTestId("pending-change-count")).not.toBeInTheDocument();
  });
});

describe("firmware assignment confirmation", () => {
  it.each(["present", "missing"])(
    "compares the saved version with the target when its catalog entry is %s",
    (catalog) => {
      const { update } = renderManage();
      if (catalog === "missing") update({ firmwareFiles: firmwareFiles.filter((file) => file.id !== "old") });
      chooseFile("next");
      fireEvent.click(screen.getByTestId("apply-firmware-changes"));
      const table = within(screen.getByTestId("apply-firmware-dialog")).getByRole("table", {
        name: "Firmware changes",
      });
      expect(
        within(table)
          .getAllByRole("columnheader")
          .map((cell) => cell.textContent),
      ).toEqual(["Model", "Original", "Target"]);
      const row = within(table).getByRole("row", { name: /proto Rig/i });
      expect(within(row).getByRole("rowheader")).toHaveTextContent(/proto Rig/i);
      expect(
        within(row)
          .getAllByRole("cell")
          .map((cell) => cell.textContent),
      ).toEqual(["old-saved", "next-catalog"]);
      expect(table).not.toHaveTextContent("old-catalog");
    },
  );

  it("shows no firmware as the original when assigning an unassigned model", () => {
    const channel = channelFor();
    channel.modelGroups = [create(ReleaseChannelModelGroupSchema, { manufacturer: "proto", model: "Rig" })];
    renderManage(channel);
    chooseFile("next");
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const table = within(screen.getByTestId("apply-firmware-dialog")).getByRole("table", { name: "Firmware changes" });
    const row = within(table).getByRole("row", { name: /Proto Rig/ });
    expect(
      within(row)
        .getAllByRole("cell")
        .map((cell) => cell.textContent),
    ).toEqual(["No firmware", "next-catalog"]);
  });

  it.each([RolloutMethod.ALL_AT_ONCE, RolloutMethod.DELEGATED])(
    "describes a clear without promising an update for method %s",
    async (method) => {
      const { onApply } = renderManage(channelFor(method));
      chooseFile("");
      expect(screen.queryByText(/changes? pending/i)).not.toBeInTheDocument();
      expect(screen.getByTestId("pending-change-count")).toHaveTextContent("1");
      fireEvent.click(screen.getByTestId("apply-firmware-changes"));
      const dialog = screen.getByTestId("apply-firmware-dialog");
      expect(dialog).toHaveTextContent("Clear firmware assignments?");
      expect(dialog).toHaveTextContent("Clear firmware assignments for 1 model in Production.");
      expect(dialog).toHaveTextContent("Clearing stops enforcement and cancels remaining updates");
      expect(dialog).toHaveTextContent("updates already dispatched may finish");
      expect(dialog).not.toHaveTextContent("Pacing:");
      const row = within(within(dialog).getByRole("table", { name: "Firmware changes" })).getByRole("row", {
        name: /proto Rig/i,
      });
      expect(
        within(row)
          .getAllByRole("cell")
          .map((cell) => cell.textContent),
      ).toEqual(["old-saved", "No firmware"]);
      expect(within(dialog).queryByRole("button", { name: "Start update" })).not.toBeInTheDocument();
      await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Clear assignments" })));
      expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: "" },
      ]);
    },
  );

  it("describes both operations for mixed firmware changes", async () => {
    const channel = channelFor();
    channel.modelGroups.push(group("Other", "second-old"));
    const { onApply } = renderManage(channel);
    chooseFile("next");
    chooseFile("", "Other");
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const dialog = screen.getByTestId("apply-firmware-dialog");
    expect(dialog).toHaveTextContent("Apply firmware changes?");
    expect(dialog).toHaveTextContent("Assign firmware for 1 model in Production.");
    expect(dialog).toHaveTextContent("Updates start where needed.");
    expect(dialog).toHaveTextContent("Clear firmware assignments for 1 model in Production.");
    expect(dialog).toHaveTextContent("Pacing: single batch.");
    expect(dialog).toHaveTextContent("updates already dispatched may finish");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Apply changes" })));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "next" },
      { manufacturer: "Proto", model: "Other", firmwareFileId: "" },
    ]);
  });
});

describe("unavailable assigned firmware", () => {
  it("uses server availability and recovers when the saved artifact returns", () => {
    const channel = channelFor();
    const { update } = renderManage(channel);
    update({ firmwareFiles: [] });
    expect(screen.queryByText(/Assigned firmware is unavailable\./)).not.toBeInTheDocument();
    update({ channel: { ...channel, modelGroups: [{ ...group(), firmwareFileId: "", firmwareAvailable: false }] } });
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("old-saved");
    expect(screen.getByText(/Assigned firmware is unavailable\./)).toHaveTextContent("same checksum");
    expect(screen.getByText("Assigned firmware unavailable")).toBeVisible();
    update({ channel: { ...channel } });
    expect(screen.queryByText(/Assigned firmware is unavailable\./)).not.toBeInTheDocument();
    expect(screen.getByText("Up to date")).toBeVisible();
  });

  it.each(["next", ""])("keeps the warning until replacement %s is acknowledged", async (id) => {
    const channel = channelFor();
    channel.modelGroups[0] = { ...group(), firmwareFileId: "", firmwareAvailable: false };
    const { update } = renderManage(channel, true);
    chooseFile(id);
    expect(screen.getByText(/Assigned firmware is unavailable\./)).toBeVisible();
    await startApply(id === "" ? "Clear assignments" : "Start update");
    expect(screen.queryByText(/Assigned firmware is unavailable\./)).not.toBeInTheDocument();
    expect(screen.getByText("Refreshing update status")).toBeVisible();
    update({ channel: { ...channel }, hasRefreshError: false });
    expect(screen.getByText(/Assigned firmware is unavailable\./)).toBeVisible();
  });
});

describe("firmware Apply with saved delegated behavior", () => {
  it("applies a supported draft method before starting firmware, including after a failed refresh", async () => {
    const { onApply, onSave } = renderManage(channelFor(RolloutMethod.DELEGATED), true);
    chooseFile("next");
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    chooseMethod("Single batch");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    expect(onSave).not.toHaveBeenCalled();
    await startApply("Apply changes");
    expect(onSave).toHaveBeenCalledOnce();
    expect(onSave.mock.invocationCallOrder[0]).toBeLessThan(onApply.mock.invocationCallOrder[0]);
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
    await startApply("Clear assignments");
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "" },
      { manufacturer: "Proto", model: "Other", firmwareFileId: "" },
    ]);
  });

  it("disables an open confirmation if the saved method becomes delegated, then allows correction", async () => {
    const channel = channelFor();
    const { update, onApply, onSave } = renderManage(channel);
    chooseFile("next");
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    update({ channel: { ...channel, behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED }) } });
    expect(screen.getByRole("button", { name: "Start update" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
    expect(onApply).not.toHaveBeenCalled();
    fireEvent.click(within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Cancel" }));
    chooseMethod("Single batch");
    await startApply("Apply changes");
    expect(onSave).toHaveBeenCalledOnce();
    expect(onApply).toHaveBeenCalledOnce();
  });
});

describe("acknowledged firmware assignments before read recovery", () => {
  it("compares against the acknowledged version when staging another change before reads recover", async () => {
    const { update } = renderManage(channelFor(), true);
    chooseFile("next");
    await startApply();
    update({ firmwareFiles: firmwareFiles.filter((file) => file.id !== "next") });
    chooseFile("later");
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const table = within(screen.getByTestId("apply-firmware-dialog")).getByRole("table", { name: "Firmware changes" });
    const row = within(table).getByRole("row", { name: /proto Rig/i });
    expect(within(row).getByRole("rowheader")).toHaveTextContent(/proto Rig/i);
    expect(
      within(row)
        .getAllByRole("cell")
        .map((cell) => cell.textContent),
    ).toEqual(["next-server", "later-catalog"]);
    expect(table).not.toHaveTextContent("old-saved");
  });

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
    expect(screen.queryByRole("button", { name: "Discard" })).not.toBeInTheDocument();
    for (const name of ["Channel settings", "History"]) {
      expect(screen.getByRole("button", { name })).toBeVisible();
    }
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
    await startApply("Clear assignments");
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
    "accepts a newer %s selection after Apply and Discard restores the acknowledged selection",
    async (newer) => {
      const channel = channelFor();
      const { onApply, update } = renderManage(channel);
      const write = deferred<Rollout[]>();
      onApply.mockReturnValueOnce(write.promise);
      chooseFile("next");
      await startApply();
      expect(screen.getByTestId("channel-firmware-select-Rig")).toBeDisabled();
      update({ channel: { ...channel }, hasRefreshError: true });
      await act(async () => write.resolve([started()]));
      chooseFile(newer);
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent(`${newer}-catalog`);
      expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
      fireEvent.click(screen.getByRole("button", { name: "Discard" }));
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("next-server");
      expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    },
  );

  it("preserves unavailable newer choices and their recovery guard after an earlier Apply", async () => {
    const { onApply, update } = renderManage(channelFor(), true);
    const write = deferred<Rollout[]>();
    onApply.mockReturnValueOnce(write.promise);
    chooseFile("next");
    await startApply();
    await act(async () => write.resolve([started()]));
    chooseFile("later");
    update({ firmwareFiles: firmwareFiles.filter((f) => f.id !== "later") });
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
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    expect(screen.getByRole("button", { name: "Discard" })).toBeEnabled();
    expect(screen.getByTestId("channel-settings")).toBeEnabled();
    for (const name of ["History"]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
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
    const write = deferred<Rollout[]>();
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
