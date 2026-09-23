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
  });

  it("retains settings, invalid numeric text, and staged firmware through close and reopen", () => {
    const props = manageViewProps();
    render(<ReleaseChannelManageView {...props} channel={canaryChannel} />);
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Canary draft" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Draft description" } });
    fireEvent.change(screen.getByLabelText("Pilot batch size (miners)"), { target: { value: "unfinished" } });
    closeChannelSettings();

    expect(screen.queryByTestId("channel-settings-modal")).not.toBeInTheDocument();
    expect(screen.getByText("Unsaved settings")).toBeVisible();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
    expect(screen.getByText("1 firmware change pending")).toBeVisible();
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Canary draft");
    expect(screen.getByLabelText("Description")).toHaveValue("Draft description");
    expect(screen.getByLabelText("Pilot batch size (miners)")).toHaveValue("unfinished");
    expect(screen.getByLabelText("Pilot batch size (miners)")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByTestId("channel-settings-modal")).not.toBeInTheDocument();
    expect(props.onSave).not.toHaveBeenCalled();
    expect(props.onApply).not.toHaveBeenCalled();
  });

  it.each(["success", "failure"])("keeps the modal open and draft accurate after save %s", async (outcome) => {
    const props = manageViewProps();
    const save = deferred<void>();
    props.onSave.mockReturnValueOnce(save.promise);
    render(<ReleaseChannelManageView {...props} channel={canaryChannel} />);
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved canary" } });
    fireEvent.click(screen.getByTestId("save-channel"));
    closeChannelSettings();
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.getByTestId("channel-settings-modal")).toBeVisible());
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    await act(async () => {
      if (outcome === "success") save.resolve();
      else save.reject(new Error("Save rejected"));
    });

    await waitFor(() => expect(screen.getByTestId("channel-settings-modal")).toBeVisible());
    expect(screen.getByLabelText("Name")).toHaveValue("Saved canary");
    expect(props.onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Saved canary" }));
    if (outcome === "success") {
      expect(screen.getByTestId("save-channel")).toBeDisabled();
      expect(screen.queryByText("Unsaved settings")).not.toBeInTheDocument();
    } else {
      expect(screen.getByTestId("save-channel")).toBeEnabled();
      expect(pushToast).toHaveBeenCalledWith({ message: "Save rejected", status: "error" });
    }
    closeChannelSettings();
    expect(screen.queryByTestId("channel-settings-modal")).not.toBeInTheDocument();
    if (outcome === "failure") expect(screen.getByText("Unsaved settings")).toBeVisible();
  });

  it("shows new-channel settings in the create modal and validates before saving", async () => {
    render(<ReleaseChannelManageView {...manageViewProps()} />);
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
