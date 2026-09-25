import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { closeChannelSettings, deferred, manageViewProps, openChannelSettings } from "./__tests__/helpers";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import { canaryChannel } from "./ReleaseChannels.fixtures";
import { pushToast } from "@/shared/features/toaster";

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

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
});
afterEach(() => vi.restoreAllMocks());

describe("channel settings modal", () => {
  it("offers deletion only after the settings sections and keeps settings open for confirmation", () => {
    const onDelete = vi.fn();
    render(<ReleaseChannelManageView {...manageViewProps()} channel={canaryChannel} onDelete={onDelete} />);

    expect(screen.queryByTestId("delete-channel")).not.toBeInTheDocument();
    openChannelSettings();
    const modal = screen.getByTestId("channel-settings-modal");
    const settings = within(modal);
    const deleteSection = settings.getByText("Delete this channel");
    expect(
      settings.getByText("Applies to").compareDocumentPosition(deleteSection) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    const deleteButton = settings.getByRole("button", { name: "Delete channel" });
    expect(deleteButton).toBeEnabled();
    fireEvent.click(deleteButton);
    expect(onDelete).toHaveBeenCalledExactlyOnceWith(canaryChannel);
    expect(modal).toBeInTheDocument();
  });

  it.each(["settings", "firmware"])("requires %s changes to be applied or discarded before deletion", (draft) => {
    const onDelete = vi.fn();
    render(<ReleaseChannelManageView {...manageViewProps()} channel={canaryChannel} onDelete={onDelete} />);
    if (draft === "settings") {
      openChannelSettings();
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved channel" } });
      closeChannelSettings();
    } else {
      fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
      fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    }
    openChannelSettings();
    const modal = within(screen.getByTestId("channel-settings-modal"));
    expect(modal.getByTestId("delete-channel")).toBeDisabled();
    expect(modal.getByText(/apply or discard/i)).toBeInTheDocument();
    fireEvent.click(modal.getByTestId("delete-channel"));
    expect(onDelete).not.toHaveBeenCalled();
    closeChannelSettings();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    openChannelSettings();
    expect(screen.getByTestId("delete-channel")).toBeEnabled();
  });

  it("disables deletion while another channel write is pending", () => {
    const props = {
      ...manageViewProps(),
      channel: canaryChannel,
      onDelete: vi.fn(),
      writeLock: { isLocked: false, tryAcquire: vi.fn(), release: vi.fn() },
    };
    const { rerender } = render(<ReleaseChannelManageView {...props} />);
    openChannelSettings();
    rerender(<ReleaseChannelManageView {...props} writeLock={{ ...props.writeLock, isLocked: true }} />);
    expect(screen.getByTestId("delete-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("delete-channel"));
    expect(props.onDelete).not.toHaveBeenCalled();
  });

  it("prioritizes assigned miners and opens the three settings sections on demand", async () => {
    render(<ReleaseChannelManageView {...manageViewProps()} channel={canaryChannel} />);

    expect(screen.getByRole("table", { name: "Assigned miners by model" })).toBeVisible();
    expect(screen.getByTestId("view-miners-Rig")).toBeVisible();
    expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
    expect(screen.queryByTestId("rollout-controls")).not.toBeInTheDocument();
    expect(screen.queryByTestId("scope-editor")).not.toBeInTheDocument();
    expect(screen.queryByTestId("save-channel")).not.toBeInTheDocument();

    openChannelSettings();
    const settings = within(screen.getByTestId("channel-settings-modal"));
    await waitFor(() => expect(settings.getByText("General")).toBeVisible());
    expect(settings.getByText("Applies to")).toBeVisible();
    expect(settings.getByText("Update behavior")).toBeVisible();
    expect(settings.getByLabelText("Name")).toHaveValue(canaryChannel.name);
    expect(settings.getByTestId("save-channel")).toBeDisabled();
    expect(settings.queryByText("Delete this channel")).not.toBeInTheDocument();
  });

  it("keeps settings editable while staging firmware and discards the entire draft", () => {
    const props = manageViewProps();
    render(<ReleaseChannelManageView {...props} channel={canaryChannel} />);
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Canary draft" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Draft description" } });
    fireEvent.change(screen.getByLabelText("Pilot batch size (miners)"), { target: { value: "unfinished" } });
    closeChannelSettings();
    expect(screen.queryByText(/changes? pending/i)).not.toBeInTheDocument();
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("3");

    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    expect(screen.queryByTestId("channel-settings-modal")).not.toBeInTheDocument();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("4");
    expect(screen.getByTestId("channel-settings")).toBeEnabled();
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Canary draft");
    expect(screen.getByLabelText("Pilot batch size (miners)")).toHaveValue("unfinished");
    closeChannelSettings();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.queryByTestId("pending-change-count")).not.toBeInTheDocument();
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue(canaryChannel.name);
    expect(screen.getByLabelText("Description")).toHaveValue(canaryChannel.description);
    expect(screen.getByLabelText("Pilot batch size (miners)")).toHaveValue(String(canaryChannel.behavior!.pilotSize));
    expect(screen.getByLabelText("Pilot batch size (miners)")).not.toHaveAttribute("aria-invalid");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByTestId("channel-settings-modal")).not.toBeInTheDocument();
    expect(props.onSave).not.toHaveBeenCalled();
    expect(props.onApply).not.toHaveBeenCalled();
  });

  it.each(["success", "failure"])("reviews settings before writing and retains the draft after %s", async (outcome) => {
    const props = manageViewProps();
    const save = deferred<void>();
    props.onSave.mockReturnValueOnce(save.promise);
    render(<ReleaseChannelManageView {...props} channel={canaryChannel} />);
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved canary" } });
    expect(screen.getByTestId("save-channel")).toHaveTextContent("Review changes");
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(props.onSave).not.toHaveBeenCalled();
    expect(screen.queryByTestId("channel-settings-modal")).not.toBeInTheDocument();
    const dialog = screen.getByTestId("apply-firmware-dialog");
    expect(dialog).toHaveTextContent("Channel settings");
    expect(dialog).toHaveTextContent("Saved canary");
    fireEvent.click(within(dialog).getByRole("button", { name: "Apply changes" }));
    fireEvent.keyDown(document, { key: "Escape" });
    expect(dialog).toBeInTheDocument();
    expect(screen.getByTestId("channel-settings")).toBeDisabled();
    await act(async () => {
      if (outcome === "success") save.resolve();
      else save.reject(new Error("Save rejected"));
    });
    expect(props.onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Saved canary" }));
    expect(props.onApply).not.toHaveBeenCalled();
    if (outcome === "failure") {
      expect(pushToast).toHaveBeenCalledWith({ message: "Save rejected", status: "error" });
      fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    }
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Saved canary");
    if (outcome === "success") expect(screen.getByTestId("save-channel")).toBeDisabled();
    else expect(screen.getByTestId("save-channel")).toBeEnabled();
  });

  it("shows new-channel settings in the create modal and validates before saving", async () => {
    render(<ReleaseChannelManageView {...manageViewProps()} onDelete={vi.fn()} />);
    const modal = within(screen.getByTestId("create-release-channel-modal"));
    expect(modal.getByText("Create release channel")).toBeInTheDocument();
    expect(modal.getByTestId("release-channel-new")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("Name")).toBeVisible());
    expect(screen.getByTestId("scope-editor")).toBeVisible();
    expect(screen.getByTestId("rollout-controls")).toBeVisible();
    expect(screen.getByTestId("save-channel")).toHaveTextContent("Create channel");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New channel" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    const offlineLimit = screen.getByLabelText("Max miners offline at once (0 for no limit)");
    fireEvent.change(offlineLimit, { target: { value: "unfinished" } });
    expect(offlineLimit).toHaveValue("unfinished");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(offlineLimit, { target: { value: "1" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    expect(screen.queryByTestId("channel-settings")).not.toBeInTheDocument();
    expect(screen.queryByTestId("channel-settings-modal")).not.toBeInTheDocument();
    expect(modal.queryByText("Delete this channel")).not.toBeInTheDocument();
    expect(modal.queryByTestId("delete-channel")).not.toBeInTheDocument();
  });

  it("blocks create-modal dismissal while another channel write holds the lock", () => {
    const onCancelCreate = vi.fn();
    const props = {
      ...manageViewProps(),
      onCancelCreate,
      writeLock: { isLocked: true, tryAcquire: vi.fn(), release: vi.fn() },
    };
    const { rerender } = render(<ReleaseChannelManageView {...props} />);
    fireEvent.click(
      within(screen.getByTestId("create-release-channel-modal")).getByRole("button", { name: "Close dialog" }),
    );
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onCancelCreate).not.toHaveBeenCalled();

    rerender(<ReleaseChannelManageView {...props} writeLock={{ ...props.writeLock, isLocked: false }} />);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onCancelCreate).toHaveBeenCalledOnce();
  });
});
