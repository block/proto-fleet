import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  applyChannelSettings,
  closeChannelSettings,
  deferred,
  openChannelSettings,
  releaseChannelsApi,
} from "./__tests__/helpers";
import { canaryChannel, firmwareFiles, productionChannel } from "./ReleaseChannels.fixtures";
import ReleaseChannelsTab from "./ReleaseChannelsTab";
import {
  type ReleaseChannel,
  ReleaseChannelSchema,
  type Rollout,
  RolloutSchema,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import { useFleetStore } from "@/protoFleet/store";

const { listFirmwareFiles, pushToast } = vi.hoisted(() => ({ listFirmwareFiles: vi.fn(), pushToast: vi.fn() }));
const historyActions = { onViewRollout: vi.fn(), onRollbackRollout: vi.fn() };

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

const apiFor = (): ReleaseChannelsApi => ({ ...releaseChannelsApi(), channels: [canaryChannel] });
const deferredCatalog = deferred<FirmwareFileInfo[]>;

const initialAuth = useFleetStore.getState().auth;
const flush = () => act(async () => {});
const poll = () => act(async () => vi.advanceTimersByTime(30_000));
const openPicker = () => {
  closeChannelSettings();
  fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
};

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

const deferredWrite = deferred<void>;

const deleteConfirm = () =>
  within(screen.getByTestId("delete-channel-dialog")).getByRole("button", { name: "Delete channel" });

const closeCreate = () =>
  fireEvent.click(
    within(screen.getByTestId("create-release-channel-modal")).getByRole("button", { name: "Close dialog" }),
  );

describe("release channel creation modal", () => {
  it.each([
    { hasExisting: false, dismiss: "close button" },
    { hasExisting: false, dismiss: "Escape" },
    { hasExisting: true, dismiss: "close button" },
    { hasExisting: true, dismiss: "Escape" },
  ])(
    "keeps the list behind creation and cancels with $dismiss (existing channels: $hasExisting)",
    async ({ hasExisting, dismiss }) => {
      const api = { ...apiFor(), channels: hasExisting ? [canaryChannel] : [] };
      render(<ReleaseChannelsTab {...historyActions} api={api} />);
      await flush();
      const background = hasExisting
        ? screen.getByTestId("channel-row-Canary")
        : screen.getByText("No release channels");
      fireEvent.click(screen.getByTestId("create-release-channel"));

      expect(background).toBeInTheDocument();
      expect(background.closest("[inert]")).not.toBeNull();
      expect(screen.getByTestId("create-release-channel-modal")).toBeInTheDocument();
      expect(screen.queryByTestId("back-to-channels")).not.toBeInTheDocument();
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Discarded draft" } });
      fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Discarded description" } });
      if (dismiss === "Escape") fireEvent.keyDown(document, { key: "Escape" });
      else closeCreate();

      expect(screen.queryByTestId("create-release-channel-modal")).not.toBeInTheDocument();
      expect(background).toBeInTheDocument();
      expect(background.closest("[inert]")).toBeNull();
      expect(api.createChannel).not.toHaveBeenCalled();
      fireEvent.click(screen.getByTestId("create-release-channel"));
      expect(screen.getByLabelText("Name")).toHaveValue("");
      expect(screen.getByLabelText("Description")).toHaveValue("");
      expect(screen.getByTestId("save-channel")).toBeDisabled();
    },
  );

  it("blocks dismissal during create, preserves a failed draft, and opens the saved channel after retry", async () => {
    const api = apiFor();
    const firstWrite = deferred<ReleaseChannel>();
    const retryWrite = deferred<ReleaseChannel>();
    const created = create(ReleaseChannelSchema, { id: 12n, name: "Retry channel", description: "Keep this draft" });
    api.createChannel = vi.fn().mockReturnValueOnce(firstWrite.promise).mockReturnValueOnce(retryWrite.promise);
    const { rerender } = render(<ReleaseChannelsTab {...historyActions} api={api} />);
    await flush();
    fireEvent.click(screen.getByTestId("create-release-channel"));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: created.name } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: created.description } });
    const save = screen.getByTestId("save-channel");
    const close = within(screen.getByTestId("create-release-channel-modal")).getByRole("button", {
      name: "Close dialog",
    });
    act(() => {
      save.click();
      save.click();
      close.click();
      fireEvent.keyDown(document, { key: "Escape" });
    });
    expect(api.createChannel).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ name: created.name, description: created.description }),
    );
    expect(screen.getByTestId("create-release-channel-modal")).toBeInTheDocument();
    expect(save).toBeDisabled();
    closeCreate();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.getByTestId("create-release-channel-modal")).toBeInTheDocument();

    await act(async () => firstWrite.reject(new Error("Channel creation failed")));
    expect(pushToast).toHaveBeenCalledWith({ message: "Channel creation failed", status: "error" });
    expect(screen.getByLabelText("Name")).toHaveValue(created.name);
    expect(screen.getByLabelText("Description")).toHaveValue(created.description);
    expect(save).toBeEnabled();
    fireEvent.click(save);
    expect(api.createChannel).toHaveBeenCalledTimes(2);
    expect(api.createChannel).toHaveBeenLastCalledWith(vi.mocked(api.createChannel).mock.calls[0][0]);
    rerender(
      <ReleaseChannelsTab
        {...historyActions}
        api={{ ...api, channels: [...api.channels, { ...created, modelGroups: [] }] }}
      />,
    );
    await act(async () => retryWrite.resolve(created));

    expect(screen.queryByTestId("create-release-channel-modal")).not.toBeInTheDocument();
    expect(screen.getByTestId("release-channel-Retry channel")).toBeInTheDocument();
    expect(screen.getByTestId("back-to-channels")).toBeInTheDocument();
    expect(screen.queryByTestId("channel-settings-modal")).not.toBeInTheDocument();
    expect(pushToast).toHaveBeenLastCalledWith({ message: "Created release channel Retry channel", status: "success" });
  });
});

describe("release channel history on demand", () => {
  const historyApi = () => {
    const api = apiFor();
    const group = { ...canaryChannel.modelGroups[0], activeRolloutId: 0n, onTargetCount: 0 };
    api.channels = [{ ...canaryChannel, modelGroups: [group] }];
    const historical = create(RolloutSchema, {
      id: 100n,
      channelId: canaryChannel.id,
      manufacturer: group.manufacturer,
      model: group.model,
      assignmentGeneration: group.assignmentGeneration,
      revision: 1n,
      status: RolloutStatus.COMPLETED_WITH_FAILURES,
      finishedAt: { seconds: 1n },
      deviceCounts: { failed: 2 },
    });
    return { api, historical };
  };
  const pendingHistory = deferred<Rollout[]>;

  it("loads old outcomes only after expansion and retains them across core polls and reopens", async () => {
    const { api, historical } = historyApi();
    const pending = pendingHistory();
    vi.mocked(api.listChannelRollouts).mockReturnValueOnce(pending.promise);
    const { rerender } = render(<ReleaseChannelsTab {...historyActions} api={api} />);
    await flush();
    expect(api.listChannelRollouts).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("channel-toggle-Canary"));
    expect(api.listChannelRollouts).toHaveBeenCalledExactlyOnceWith(canaryChannel.id, expect.any(AbortSignal));
    expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent("Loading update history");
    await act(async () => pending.resolve([historical]));
    expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent("2 failed to update");
    rerender(<ReleaseChannelsTab {...historyActions} api={{ ...api, rollouts: [] }} />);
    expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent("2 failed to update");
    fireEvent.click(screen.getByTestId("channel-toggle-Canary"));
    fireEvent.click(screen.getByTestId("channel-toggle-Canary"));
    await flush();
    expect(api.listChannelRollouts).toHaveBeenCalledOnce();
    expect(screen.getByTestId("model-status-Canary-Rig")).toHaveTextContent("2 failed to update");
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    await flush();
    expect(api.listChannelRollouts).toHaveBeenCalledOnce();
    expect(screen.getByTestId("model-group-Rig")).toHaveTextContent("2 failed to update");
  });

  it("cancels collapsed and closed manage requests and ignores their late failures", async () => {
    const { api } = historyApi();
    const expanded = pendingHistory();
    const managed = pendingHistory();
    vi.mocked(api.listChannelRollouts).mockReturnValueOnce(expanded.promise).mockReturnValueOnce(managed.promise);
    render(<ReleaseChannelsTab {...historyActions} api={api} />);
    await flush();
    fireEvent.click(screen.getByTestId("channel-toggle-Canary"));
    const expandedSignal = vi.mocked(api.listChannelRollouts).mock.calls[0][1]!;
    fireEvent.click(screen.getByTestId("channel-toggle-Canary"));
    expect(expandedSignal.aborted).toBe(true);
    await act(async () => expanded.reject(new Error("Closed expansion failed")));
    expect(screen.queryByTestId(`channel-history-error-${canaryChannel.id}`)).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    expect(api.listChannelRollouts).toHaveBeenCalledTimes(2);
    const managedSignal = vi.mocked(api.listChannelRollouts).mock.calls[1][1]!;
    expect(screen.getByTestId("model-group-Rig")).toHaveTextContent("Loading update history");
    fireEvent.click(screen.getByTestId("back-to-channels"));
    expect(managedSignal.aborted).toBe(true);
    await act(async () => managed.reject(new Error("Closed management failed")));
    expect(screen.queryByText(/Closed management failed/)).not.toBeInTheDocument();
    expect(api.listChannelRollouts).toHaveBeenCalledTimes(2);
  });

  it("keeps settings writable after a history failure and recovers through a separate retry", async () => {
    const { api, historical } = historyApi();
    vi.mocked(api.listChannelRollouts)
      .mockRejectedValueOnce(new Error("History service unavailable"))
      .mockResolvedValueOnce([historical]);
    render(<ReleaseChannelsTab {...historyActions} api={api} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    expect(screen.getByTestId("model-group-Rig")).toHaveTextContent("Update history unavailable");
    expect(screen.getByTestId(`channel-history-error-${canaryChannel.id}`)).toHaveTextContent(
      "History service unavailable",
    );
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Updated channel" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await applyChannelSettings();
    expect(api.updateChannel).toHaveBeenCalledOnce();
    closeChannelSettings();
    fireEvent.click(screen.getByRole("button", { name: "Retry update history" }));
    await flush();
    expect(api.listChannelRollouts).toHaveBeenCalledTimes(2);
    expect(screen.queryByTestId(`channel-history-error-${canaryChannel.id}`)).not.toBeInTheDocument();
    expect(screen.getByTestId("model-group-Rig")).toHaveTextContent("2 failed to update");
  });
});

describe("release channel deletion coordination", () => {
  it("blocks Delete during settings Apply and its follow-up read, including actions before a rerender", async () => {
    const api = apiFor();
    const committed = deferredWrite();
    const refreshed = deferredWrite();
    api.updateChannel = vi.fn(async () => {
      await committed.promise;
      await refreshed.promise;
      return undefined;
    });
    render(<ReleaseChannelsTab {...historyActions} api={api} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    fireEvent.click(screen.getByTestId("delete-channel"));
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved channel" } });
    expect(screen.queryByTestId("delete-channel")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(api.updateChannel).not.toHaveBeenCalled();
    const apply = within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" });
    const confirm = deleteConfirm();
    const back = screen.getByTestId("back-to-channels");
    act(() => {
      apply.click();
      confirm.click();
      back.click();
    });
    expect(api.updateChannel).toHaveBeenCalledOnce();
    expect(api.deleteChannel).not.toHaveBeenCalled();
    expect(screen.queryByTestId("delete-channel")).not.toBeInTheDocument();
    expect(screen.getByTestId("channel-settings")).toBeDisabled();
    expect(confirm).toBeDisabled();
    expect(back).toBeDisabled();
    await act(async () => committed.resolve());
    expect(confirm).toBeDisabled();
    expect(screen.getByTestId("channel-settings")).toBeDisabled();
    expect(api.updateChannel).toHaveBeenCalledWith(
      canaryChannel.id,
      expect.objectContaining({ name: "Saved channel" }),
    );
    await act(async () => refreshed.resolve());
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Saved channel");
    closeChannelSettings();
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
    render(<ReleaseChannelsTab {...historyActions} api={api} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    fireEvent.click(screen.getByTestId("delete-channel"));
    openPicker();
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const start = within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", {
      name: "Clear assignments",
    });
    const confirm = deleteConfirm();
    act(() => {
      start.click();
      confirm.click();
    });
    expect(api.applyFirmware).toHaveBeenCalledOnce();
    expect(api.deleteChannel).not.toHaveBeenCalled();
    expect(screen.queryByTestId("delete-channel")).not.toBeInTheDocument();
    expect(confirm).toBeDisabled();
    await act(async () => applied.resolve());
    expect(deleteConfirm()).toBeEnabled();
    await act(async () => fireEvent.click(deleteConfirm()));
    expect(api.deleteChannel).toHaveBeenCalledOnce();
  });

  it("serializes Delete against combined Apply and duplicate confirmation, retaining drafts and retry after failure", async () => {
    const api = apiFor();
    const deleted = deferredWrite();
    api.deleteChannel = vi.fn().mockReturnValueOnce(deleted.promise).mockResolvedValue(undefined);
    render(<ReleaseChannelsTab {...historyActions} api={api} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    fireEvent.click(screen.getByTestId("delete-channel"));
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved name" } });
    openPicker();
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const start = within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", {
      name: "Apply changes",
    });
    openChannelSettings();
    const review = screen.getByTestId("save-channel");
    const confirm = deleteConfirm();
    const cancel = within(screen.getByTestId("delete-channel-dialog")).getByRole("button", { name: "Cancel" });
    act(() => {
      confirm.click();
      confirm.click();
      start.click();
      cancel.click();
      screen.getByTestId("back-to-channels").click();
    });
    expect(api.deleteChannel).toHaveBeenCalledOnce();
    expect(api.updateChannel).not.toHaveBeenCalled();
    expect(api.applyFirmware).not.toHaveBeenCalled();
    expect(review).toBeDisabled();
    expect(start).toBeDisabled();
    expect(screen.getByTestId("delete-channel-dialog")).toBeInTheDocument();
    await act(async () => deleted.reject(new Error("Channel deletion failed")));
    expect(pushToast).toHaveBeenCalledWith({ message: "Channel deletion failed", status: "error" });
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Unsaved name");
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
    expect(review).toBeEnabled();
    expect(start).toBeEnabled();
    expect(deleteConfirm()).toBeEnabled();
    await act(async () => fireEvent.click(deleteConfirm()));
    expect(api.deleteChannel).toHaveBeenCalledTimes(2);
    expect(pushToast).toHaveBeenLastCalledWith({ message: "Deleted release channel Canary", status: "success" });
    expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
  });
});

describe("acknowledged channel writes", () => {
  it("keeps multiple committed deletions hidden until each is confirmed by a successful snapshot", async () => {
    const api = { ...apiFor(), channels: [canaryChannel, productionChannel] };
    const deleted = deferredWrite();
    api.deleteChannel = vi.fn().mockReturnValueOnce(deleted.promise).mockResolvedValue(undefined);
    const { rerender } = render(
      <ReleaseChannelsTab {...historyActions} api={api} initialManagedChannelId={canaryChannel.id} />,
    );
    await flush();
    fireEvent.click(screen.getByTestId("delete-channel"));
    fireEvent.click(deleteConfirm());
    const failed = { ...api, error: new Error("Refresh failed") };
    rerender(<ReleaseChannelsTab {...historyActions} api={failed} initialManagedChannelId={canaryChannel.id} />);
    await act(async () => deleted.resolve());
    expect(screen.queryByTestId("channel-row-Canary")).not.toBeInTheDocument();
    expect(screen.getByTestId("manage-channel-Production")).toBeInTheDocument();
    // An unrelated successful-looking render still contains the deleted row.
    rerender(<ReleaseChannelsTab {...historyActions} api={{ ...api, channels: [...api.channels] }} />);
    await flush();
    expect(screen.queryByTestId("channel-row-Canary")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("manage-channel-Production"));
    fireEvent.click(screen.getByTestId("delete-channel"));
    await act(async () => fireEvent.click(deleteConfirm()));
    rerender(<ReleaseChannelsTab {...historyActions} api={{ ...failed, channels: [...api.channels] }} />);
    await flush();
    expect(screen.queryByTestId("channel-row-Canary")).not.toBeInTheDocument();
    expect(screen.queryByTestId("channel-row-Production")).not.toBeInTheDocument();
    expect(api.deleteChannel).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("channel-write-pending")).toBeInTheDocument();

    rerender(<ReleaseChannelsTab {...historyActions} api={{ ...api, channels: [productionChannel] }} />);
    await flush();
    expect(screen.queryByTestId("channel-row-Production")).not.toBeInTheDocument();
    expect(screen.getByTestId("channel-write-pending")).toBeInTheDocument();
    rerender(<ReleaseChannelsTab {...historyActions} api={{ ...api, channels: [] }} />);
    expect(screen.queryByTestId("channel-write-pending")).not.toBeInTheDocument();
    expect(screen.getByText("No release channels")).toBeInTheDocument();
  });

  it.each([false, true])(
    "blocks another create after Back while the committed channel is pending (existing channels: %s)",
    async (hasExisting) => {
      const api = { ...apiFor(), channels: hasExisting ? [canaryChannel] : [] };
      const committed = deferredWrite();
      const created = create(ReleaseChannelSchema, { id: 12n, name: "Created once" });
      api.createChannel = vi.fn(async () => {
        await committed.promise;
        return created;
      });
      const { rerender } = render(<ReleaseChannelsTab {...historyActions} api={api} />);
      await flush();
      fireEvent.click(screen.getByTestId("create-release-channel"));
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: created.name } });
      fireEvent.click(screen.getByTestId("save-channel"));
      // An older poll finishes while the mutation's own follow-up read is pending.
      const olderSnapshot = [...api.channels];
      rerender(<ReleaseChannelsTab {...historyActions} api={{ ...api, channels: olderSnapshot }} />);
      const failed = { ...api, channels: olderSnapshot, error: new Error("Refresh failed") };
      rerender(<ReleaseChannelsTab {...historyActions} api={failed} />);
      await act(async () => committed.resolve());
      expect(screen.getByText("Channel details will appear after a successful refresh.")).toBeInTheDocument();
      fireEvent.click(screen.getByTestId("back-to-channels"));
      expect(screen.queryByText("No release channels")).not.toBeInTheDocument();
      expect(screen.getByTestId("channel-write-pending")).toHaveTextContent(created.name);
      const createButton = screen.queryByTestId("create-release-channel");
      if (createButton) fireEvent.click(createButton);
      expect(screen.queryByTestId("release-channel-new")).not.toBeInTheDocument();
      expect(api.createChannel).toHaveBeenCalledOnce();

      // Clearing an error without a newer snapshot does not confirm the create.
      rerender(<ReleaseChannelsTab {...historyActions} api={{ ...api, channels: olderSnapshot }} />);
      expect(screen.getByTestId("channel-write-pending")).toBeInTheDocument();
      const retry = deferredWrite();
      api.refresh = vi.fn().mockReturnValue(retry.promise);
      rerender(<ReleaseChannelsTab {...historyActions} api={{ ...failed, refresh: api.refresh }} />);
      fireEvent.click(screen.getByRole("button", { name: "Refresh channel list" }));
      fireEvent.click(screen.getByRole("button", { name: "Refreshing..." }));
      expect(api.refresh).toHaveBeenCalledOnce();
      await act(async () => retry.reject(new Error("Still unavailable")));
      expect(screen.getByTestId("channel-write-pending")).toBeInTheDocument();

      rerender(
        <ReleaseChannelsTab
          {...historyActions}
          api={{ ...api, channels: [...api.channels, { ...created, modelGroups: [] }] }}
        />,
      );
      expect(screen.queryByTestId("channel-write-pending")).not.toBeInTheDocument();
      if (screen.queryByTestId("back-to-channels")) fireEvent.click(screen.getByTestId("back-to-channels"));
      await flush();
      expect(screen.getByTestId("channel-row-Created once")).toBeInTheDocument();
      fireEvent.click(screen.getByTestId("create-release-channel"));
      expect(screen.getByTestId("release-channel-new")).toBeInTheDocument();
      expect(api.createChannel).toHaveBeenCalledOnce();
    },
  );

  it("does not carry a deleted-channel acknowledgement into a replacement session", async () => {
    const api = apiFor();
    const { rerender } = render(
      <ReleaseChannelsTab {...historyActions} api={api} initialManagedChannelId={canaryChannel.id} />,
    );
    await flush();
    fireEvent.click(screen.getByTestId("delete-channel"));
    await act(async () => fireEvent.click(deleteConfirm()));
    expect(screen.queryByTestId("channel-row-Canary")).not.toBeInTheDocument();
    act(() => useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, sessionGeneration: 2 } }));
    rerender(<ReleaseChannelsTab {...historyActions} api={api} />);
    await flush();
    expect(screen.getByTestId("channel-row-Canary")).toBeInTheDocument();
    expect(screen.queryByTestId("channel-write-pending")).not.toBeInTheDocument();
  });

  it.each(["create", "delete"] as const)(
    "ignores a late %s acknowledgement after the session changes",
    async (operation) => {
      const api = apiFor();
      const committed = deferredWrite();
      const created = create(ReleaseChannelSchema, { id: 12n, name: "Old session channel" });
      api.createChannel = vi.fn(async () => {
        await committed.promise;
        return created;
      });
      api.deleteChannel = vi.fn().mockReturnValue(committed.promise);
      render(<ReleaseChannelsTab {...historyActions} api={api} />);
      await flush();
      if (operation === "create") {
        fireEvent.click(screen.getByTestId("create-release-channel"));
        fireEvent.change(screen.getByLabelText("Name"), { target: { value: created.name } });
        fireEvent.click(screen.getByTestId("save-channel"));
      } else {
        fireEvent.click(screen.getByTestId("manage-channel-Canary"));
        fireEvent.click(screen.getByTestId("delete-channel"));
        fireEvent.click(deleteConfirm());
      }
      act(() => useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, sessionGeneration: 2 } }));
      await act(async () => committed.resolve());
      expect(screen.queryByTestId("channel-write-pending")).not.toBeInTheDocument();
      if (operation === "create") closeCreate();
      else fireEvent.click(screen.getByTestId("back-to-channels"));
      await flush();
      expect(screen.getByTestId("channel-row-Canary")).toBeInTheDocument();
      fireEvent.click(screen.getByTestId("create-release-channel"));
      expect(screen.getByTestId("release-channel-new")).toBeInTheDocument();
    },
  );
});

describe("release channel firmware catalog", () => {
  it("retries an initial failure once and accepts an empty catalog without losing the draft", async () => {
    listFirmwareFiles.mockRejectedValueOnce(new Error("Catalog unavailable"));
    render(<ReleaseChannelsTab {...historyActions} api={apiFor()} initialManagedChannelId={canaryChannel.id} />);
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load firmware files");
    expect(screen.getByRole("alert")).toHaveTextContent("Catalog unavailable");
    openChannelSettings();
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
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toBe(nameInput);
    expect(nameInput).toHaveValue("Unsaved channel name");
    openPicker();
    expect(screen.getAllByRole("option")).toHaveLength(1);
    expect(screen.getByRole("option", { name: "No firmware" })).toBeInTheDocument();
  });

  it("refreshes uploads, deletions, and retargets while preserving settings and staged clear", async () => {
    const api = apiFor();
    const { rerender } = render(
      <ReleaseChannelsTab {...historyActions} api={api} initialManagedChannelId={canaryChannel.id} />,
    );
    await flush();
    openChannelSettings();
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
      <ReleaseChannelsTab
        {...historyActions}
        api={{ ...api, channels: [...api.channels] }}
        initialManagedChannelId={canaryChannel.id}
      />,
    );
    await poll();

    expect(listFirmwareFiles).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("option", { name: "No firmware" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("option", { name: /1\.4\.3/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /1\.4\.4/ })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: /1\.4\.5/ })).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Keep this draft");
    closeChannelSettings();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue(canaryChannel.name);
  });

  it("retains the last complete catalog on a refresh failure and recovers on the next poll", async () => {
    render(<ReleaseChannelsTab {...historyActions} api={apiFor()} initialManagedChannelId={canaryChannel.id} />);
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
      render(<ReleaseChannelsTab {...historyActions} api={api} initialManagedChannelId={canaryChannel.id} />);
      await flush();
      openChannelSettings();
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
      fireEvent.click(
        within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" }),
      );
      await flush();

      expect(api.updateChannel).toHaveBeenCalledExactlyOnceWith(
        canaryChannel.id,
        expect.objectContaining({ name: "Keep this draft" }),
      );
      expect(api.applyFirmware).toHaveBeenCalledExactlyOnceWith(canaryChannel.id, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: replacement.id },
      ]);
      openChannelSettings();
      expect(screen.getByLabelText("Name")).toHaveValue("Keep this draft");
    },
  );

  it.each(["resolve", "reject"] as const)("ignores an obsolete request's late %s after a new login", async (result) => {
    render(<ReleaseChannelsTab {...historyActions} api={apiFor()} initialManagedChannelId={canaryChannel.id} />);
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
    render(<ReleaseChannelsTab {...historyActions} api={apiFor()} initialManagedChannelId={canaryChannel.id} />);
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
    const { unmount } = render(<ReleaseChannelsTab {...historyActions} api={apiFor()} />);
    const signal = listFirmwareFiles.mock.calls[0][0] as AbortSignal;
    unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve(firmwareFiles));
    await poll();
    expect(listFirmwareFiles).toHaveBeenCalledOnce();

    act(() => useFleetStore.getState().auth.logout());
    render(<ReleaseChannelsTab {...historyActions} api={apiFor()} />);
    await poll();
    expect(listFirmwareFiles).toHaveBeenCalledOnce();
  });
});
