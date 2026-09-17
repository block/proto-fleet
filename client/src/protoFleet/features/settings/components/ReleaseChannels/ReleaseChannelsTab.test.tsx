import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { canaryChannel, firmwareFiles } from "./ReleaseChannels.fixtures";
import ReleaseChannelsTab from "./ReleaseChannelsTab";
import { PreviewReleaseChannelScopeResponseSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import { useFleetStore } from "@/protoFleet/store";

const { listFirmwareFiles, pushToast } = vi.hoisted(() => ({ listFirmwareFiles: vi.fn(), pushToast: vi.fn() }));

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({ useFirmwareApi: () => ({ listFirmwareFiles }) }));
vi.mock("@/shared/features/toaster", () => ({
  pushToast,
  STATUSES: { success: "success", error: "error" },
}));
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: () => null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
}));

const apiFor = (): ReleaseChannelsApi => ({
  channels: [canaryChannel],
  rollouts: [],
  minerNames: {},
  isLoading: false,
  hasLoaded: true,
  error: null,
  refresh: vi.fn().mockResolvedValue(undefined),
  createChannel: vi.fn().mockResolvedValue(undefined),
  updateChannel: vi.fn().mockResolvedValue(undefined),
  deleteChannel: vi.fn().mockResolvedValue(undefined),
  previewScope: vi.fn().mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema)),
  listChannelMiners: vi.fn().mockResolvedValue([]),
  listRolloutDevices: vi.fn().mockResolvedValue([]),
  applyFirmware: vi.fn().mockResolvedValue([]),
  rollbackFirmware: vi.fn().mockResolvedValue([]),
  continueRollout: vi.fn().mockResolvedValue(undefined),
  pauseRollout: vi.fn().mockResolvedValue(undefined),
  resumeRollout: vi.fn().mockResolvedValue(undefined),
  cancelRollout: vi.fn().mockResolvedValue(undefined),
  retryFailedDevices: vi.fn().mockResolvedValue(undefined),
});

function deferredCatalog() {
  let resolve!: (files: FirmwareFileInfo[]) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<FirmwareFileInfo[]>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

const initialAuth = useFleetStore.getState().auth;
const flush = () => act(async () => {});
const poll = () => act(async () => vi.advanceTimersByTime(30_000));
const openPicker = () => fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));

beforeEach(() => {
  vi.useFakeTimers();
  pushToast.mockClear();
  listFirmwareFiles.mockReset().mockResolvedValue(firmwareFiles);
  useFleetStore.setState({
    auth: { ...initialAuth, isAuthenticated: true, username: "operator", sessionGeneration: 1 },
  });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
});

afterEach(() => {
  cleanup();
  useFleetStore.setState({ auth: initialAuth });
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function deferredWrite() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

const deleteConfirm = () =>
  within(screen.getByTestId("delete-channel-dialog")).getByRole("button", { name: "Delete channel" });

describe("release channel deletion coordination", () => {
  it("blocks Delete during Save and its follow-up read, including actions before a rerender", async () => {
    const api = apiFor();
    const committed = deferredWrite();
    const refreshed = deferredWrite();
    api.updateChannel = vi.fn(async () => {
      await committed.promise;
      await refreshed.promise;
      return undefined;
    });
    render(<ReleaseChannelsTab api={api} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved channel" } });
    fireEvent.click(screen.getByTestId("delete-channel"));
    const save = screen.getByTestId("save-channel");
    const confirm = deleteConfirm();
    const back = screen.getByTestId("back-to-channels");
    act(() => {
      save.click();
      confirm.click();
      back.click();
    });
    expect(api.updateChannel).toHaveBeenCalledOnce();
    expect(api.deleteChannel).not.toHaveBeenCalled();
    expect(screen.getByTestId("delete-channel")).toBeDisabled();
    expect(confirm).toBeDisabled();
    expect(back).toBeDisabled();
    await act(async () => committed.resolve());
    expect(confirm).toBeDisabled();
    expect(screen.getByLabelText("Name")).toHaveValue("Saved channel");
    await act(async () => refreshed.resolve());
    expect(deleteConfirm()).toBeEnabled();
    await act(async () => fireEvent.click(deleteConfirm()));
    expect(api.deleteChannel).toHaveBeenCalledExactlyOnceWith(canaryChannel.id);
  });

  it("blocks Delete while firmware Apply is pending and leaves the existing confirmation available afterward", async () => {
    const api = apiFor();
    const applied = deferredWrite();
    api.applyFirmware = vi.fn(async () => {
      await applied.promise;
      return [];
    });
    render(<ReleaseChannelsTab api={api} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    openPicker();
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    fireEvent.click(screen.getByTestId("delete-channel"));
    const start = within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Start update" });
    const confirm = deleteConfirm();
    act(() => {
      start.click();
      confirm.click();
    });
    expect(api.applyFirmware).toHaveBeenCalledOnce();
    expect(api.deleteChannel).not.toHaveBeenCalled();
    expect(screen.getByTestId("delete-channel")).toBeDisabled();
    expect(confirm).toBeDisabled();
    await act(async () => applied.resolve());
    expect(deleteConfirm()).toBeEnabled();
    await act(async () => fireEvent.click(deleteConfirm()));
    expect(api.deleteChannel).toHaveBeenCalledOnce();
  });

  it("serializes Delete against Save, Apply and duplicate confirmation, retaining drafts and retry after failure", async () => {
    const api = apiFor();
    const deleted = deferredWrite();
    api.deleteChannel = vi.fn().mockReturnValueOnce(deleted.promise).mockResolvedValue(undefined);
    render(<ReleaseChannelsTab api={api} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved name" } });
    openPicker();
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    fireEvent.click(screen.getByTestId("delete-channel"));
    const save = screen.getByTestId("save-channel");
    const start = within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Start update" });
    const confirm = deleteConfirm();
    const cancel = within(screen.getByTestId("delete-channel-dialog")).getByRole("button", { name: "Cancel" });
    act(() => {
      confirm.click();
      confirm.click();
      save.click();
      start.click();
      cancel.click();
      screen.getByTestId("back-to-channels").click();
    });
    expect(api.deleteChannel).toHaveBeenCalledOnce();
    expect(api.updateChannel).not.toHaveBeenCalled();
    expect(api.applyFirmware).not.toHaveBeenCalled();
    expect(save).toBeDisabled();
    expect(start).toBeDisabled();
    expect(screen.getByTestId("delete-channel-dialog")).toBeInTheDocument();
    await act(async () => deleted.reject(new Error("Channel deletion failed")));
    expect(pushToast).toHaveBeenCalledWith({ message: "Channel deletion failed", status: "error" });
    expect(screen.getByLabelText("Name")).toHaveValue("Unsaved name");
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
    expect(save).toBeEnabled();
    expect(start).toBeEnabled();
    expect(deleteConfirm()).toBeEnabled();
    await act(async () => fireEvent.click(deleteConfirm()));
    expect(api.deleteChannel).toHaveBeenCalledTimes(2);
    expect(pushToast).toHaveBeenLastCalledWith({ message: "Deleted release channel Canary", status: "success" });
    expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
  });
});

describe("release channel firmware catalog", () => {
  it("retries an initial failure once and accepts an empty catalog without losing the draft", async () => {
    listFirmwareFiles.mockRejectedValueOnce(new Error("Catalog unavailable"));
    render(<ReleaseChannelsTab api={apiFor()} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load firmware files");
    expect(screen.getByRole("alert")).toHaveTextContent("Catalog unavailable");
    const nameInput = screen.getByLabelText("Name");
    fireEvent.change(nameInput, { target: { value: "Unsaved channel name" } });
    const retry = deferredCatalog();
    listFirmwareFiles.mockReturnValueOnce(retry.promise);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(screen.getByRole("alert")).toHaveAttribute("aria-busy", "true");
    fireEvent.click(screen.getByRole("button", { name: "Retrying..." }));
    expect(listFirmwareFiles).toHaveBeenCalledTimes(2);
    await act(async () => retry.resolve([]));

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByText("Loading firmware files...")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toBe(nameInput);
    expect(nameInput).toHaveValue("Unsaved channel name");
    openPicker();
    expect(screen.getAllByRole("option")).toHaveLength(1);
    expect(screen.getByRole("option", { name: "No firmware" })).toBeInTheDocument();
  });

  it("refreshes uploads, deletions, and retargets while preserving the open form and staged clear", async () => {
    const api = apiFor();
    const { rerender } = render(<ReleaseChannelsTab api={api} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    const nameInput = screen.getByLabelText("Name");
    fireEvent.change(nameInput, { target: { value: "Keep this draft" } });
    openPicker();
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    openPicker();
    expect(screen.getByRole("option", { name: /1\.4\.3/ })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /1\.4\.4/ })).toBeInTheDocument();
    listFirmwareFiles.mockResolvedValue([
      { ...firmwareFiles[1], target_model: "Other model" },
      { ...firmwareFiles[0], id: "new-upload", filename: "rig-1.4.5.swu", firmware_version: "1.4.5" },
    ]);

    rerender(
      <ReleaseChannelsTab api={{ ...api, channels: [...api.channels] }} initialManagedChannelId={canaryChannel.id} />,
    );
    await poll();

    expect(listFirmwareFiles).toHaveBeenCalledTimes(2);
    expect(screen.getByLabelText("Name")).toBe(nameInput);
    expect(nameInput).toHaveValue("Keep this draft");
    expect(screen.getByRole("option", { name: "No firmware" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("option", { name: /1\.4\.3/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /1\.4\.4/ })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: /1\.4\.5/ })).toBeInTheDocument();
  });

  it("retains the last complete catalog on a refresh failure and recovers on the next poll", async () => {
    render(<ReleaseChannelsTab api={apiFor()} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    openPicker();
    listFirmwareFiles.mockRejectedValueOnce(new Error("Temporary outage"));
    await poll();

    expect(screen.getByRole("alert")).toHaveTextContent("Firmware files may be out of date");
    expect(screen.getByRole("alert")).toHaveTextContent("Showing the last loaded firmware files");
    expect(screen.getByRole("option", { name: /1\.4\.3/ })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /1\.4\.4/ })).toBeInTheDocument();
    await poll();

    expect(listFirmwareFiles).toHaveBeenCalledTimes(3);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: /1\.4\.4/ })).toBeInTheDocument();
  });

  it.each(["deleted", "retargeted"] as const)(
    "blocks a staged file that is %s during polling and recovers through the picker without losing the draft",
    async (change) => {
      const api = apiFor();
      render(<ReleaseChannelsTab api={api} initialManagedChannelId={canaryChannel.id} />);
      await flush();
      const nameInput = screen.getByLabelText("Name");
      fireEvent.change(nameInput, { target: { value: "Keep this draft" } });
      openPicker();
      fireEvent.click(screen.getByRole("option", { name: /1\.4\.3/ }));
      expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
      const replacement = {
        ...firmwareFiles[0],
        id: "new-upload",
        filename: "rig-1.4.5.swu",
        firmware_version: "1.4.5",
      };
      listFirmwareFiles.mockResolvedValue([
        ...firmwareFiles.slice(1),
        replacement,
        ...(change === "retargeted"
          ? [{ ...firmwareFiles[0], target_manufacturer: "Bitmain", target_model: "S21" }]
          : []),
      ]);
      await poll();

      expect(screen.getByLabelText("Name")).toBe(nameInput);
      expect(nameInput).toHaveValue("Keep this draft");
      expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
      expect(screen.getByRole("alert")).toHaveTextContent(
        "Selected firmware is unavailable for this model. Choose another version or discard the pending changes.",
      );
      fireEvent.click(screen.getByTestId("apply-firmware-changes"));
      expect(api.applyFirmware).not.toHaveBeenCalled();
      openPicker();
      expect(screen.queryByRole("option", { name: /1\.4\.3/ })).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("option", { name: /1\.4\.5/ }));

      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
      fireEvent.click(screen.getByTestId("apply-firmware-changes"));
      fireEvent.click(screen.getByRole("button", { name: "Start update" }));
      await flush();

      expect(api.applyFirmware).toHaveBeenCalledExactlyOnceWith(canaryChannel.id, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: replacement.id },
      ]);
      expect(nameInput).toHaveValue("Keep this draft");
    },
  );

  it.each(["resolve", "reject"] as const)("ignores an obsolete request's late %s after a new login", async (result) => {
    render(<ReleaseChannelsTab api={apiFor()} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    openPicker();
    const obsolete = deferredCatalog();
    listFirmwareFiles.mockReturnValueOnce(obsolete.promise);
    await poll();
    const oldSignal = listFirmwareFiles.mock.calls[1][0] as AbortSignal;
    const current = deferredCatalog();
    listFirmwareFiles.mockReturnValueOnce(current.promise);
    act(() => {
      useFleetStore.setState({
        auth: { ...useFleetStore.getState().auth, username: "new-operator", sessionGeneration: 2 },
      });
    });

    expect(oldSignal.aborted).toBe(true);
    expect(screen.queryByRole("option", { name: /1\.4\.3/ })).not.toBeInTheDocument();
    await act(async () => current.resolve([]));
    await act(async () => {
      if (result === "resolve") obsolete.resolve(firmwareFiles);
      else obsolete.reject(new Error("Old session failed"));
    });

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getAllByRole("option")).toHaveLength(1);
    expect(listFirmwareFiles).toHaveBeenCalledTimes(3);
  });

  it("times out a stalled request without starting an overlapping poll, then permits a retry", async () => {
    listFirmwareFiles.mockImplementationOnce(
      (signal: AbortSignal) =>
        new Promise<FirmwareFileInfo[]>((_, reject) => {
          signal.addEventListener("abort", () => reject(signal.reason), { once: true });
        }),
    );
    render(<ReleaseChannelsTab api={apiFor()} initialManagedChannelId={canaryChannel.id} />);
    expect(screen.getByText("Loading firmware files...")).toBeInTheDocument();
    await poll();

    expect(listFirmwareFiles).toHaveBeenCalledOnce();
    expect(screen.getByRole("alert")).toHaveTextContent("request timed out");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await flush();

    expect(listFirmwareFiles).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    openPicker();
    expect(screen.getByRole("option", { name: /1\.4\.4/ })).toBeInTheDocument();
  });

  it("aborts pending requests and stops polling on unmount or logout", async () => {
    const pending = deferredCatalog();
    listFirmwareFiles.mockReturnValueOnce(pending.promise);
    const { unmount } = render(<ReleaseChannelsTab api={apiFor()} />);
    const signal = listFirmwareFiles.mock.calls[0][0] as AbortSignal;
    unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve(firmwareFiles));
    await poll();
    expect(listFirmwareFiles).toHaveBeenCalledOnce();

    act(() => useFleetStore.getState().auth.logout());
    render(<ReleaseChannelsTab api={apiFor()} />);
    await poll();
    expect(listFirmwareFiles).toHaveBeenCalledOnce();
  });
});
