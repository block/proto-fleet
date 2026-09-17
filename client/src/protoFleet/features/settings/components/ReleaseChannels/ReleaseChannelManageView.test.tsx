import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { defaultBehavior } from "./behaviorUtils";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  PreviewReleaseChannelScopeResponseSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
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
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
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
