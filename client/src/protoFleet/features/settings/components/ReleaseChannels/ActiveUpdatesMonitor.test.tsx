import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { deferred } from "./__tests__/helpers";
import ActiveUpdatesMonitor, { type MonitorRequest } from "./ActiveUpdatesMonitor";
import {
  activeRigRollout,
  canaryChannel,
  canaryPreview,
  canceledRemainingRigRollout,
  completedRigRollout,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import { isActive } from "./rolloutStatus";
import { type Rollout, RolloutDeviceCountsSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import Firmware from "@/protoFleet/features/settings/components/Firmware";
import { useFleetStore } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";

const { mockUseReleaseChannels, mockListFirmwareFiles } = vi.hoisted(() => ({
  mockUseReleaseChannels: vi.fn(),
  mockListFirmwareFiles: vi.fn(),
}));

vi.mock("@/protoFleet/api/useReleaseChannels", () => ({ useReleaseChannels: mockUseReleaseChannels }));
vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({ listFirmwareFiles: mockListFirmwareFiles }),
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));
// Selection dialogs load unrelated fleet data; keep the history, detail,
// confirmation dialogs and the page-to-monitor handoff real.
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: () => null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
}));

function apiFor(rollout: Rollout) {
  return {
    channels: [
      {
        ...canaryChannel,
        modelGroups: canaryChannel.modelGroups.map((group) =>
          group.manufacturer === rollout.manufacturer && group.model === rollout.model
            ? {
                ...group,
                assignmentGeneration: rollout.assignmentGeneration,
                firmwareChecksum: rollout.firmwareChecksum,
                activeRolloutId: isActive(rollout) ? rollout.id : 0n,
              }
            : group,
        ),
      },
    ],
    rollouts: [rollout],
    acknowledgedRollbacks: [] as readonly Rollout[],
    minerNames: {},
    isLoading: false,
    hasLoaded: true,
    error: null,
    refresh: vi.fn().mockResolvedValue(undefined),
    createChannel: vi.fn().mockResolvedValue(undefined),
    updateChannel: vi.fn().mockResolvedValue(undefined),
    deleteChannel: vi.fn().mockResolvedValue(undefined),
    previewScope: vi.fn().mockResolvedValue(canaryPreview),
    listChannelMiners: vi.fn().mockResolvedValue([]),
    listChannelRollouts: vi.fn().mockResolvedValue([rollout]),
    listRolloutDevices: vi.fn().mockResolvedValue([]),
    applyFirmware: vi.fn().mockResolvedValue([]),
    rollbackFirmware: vi.fn().mockResolvedValue([]),
    continueRollout: vi.fn().mockResolvedValue(undefined),
    pauseRollout: vi.fn().mockResolvedValue(undefined),
    resumeRollout: vi.fn().mockResolvedValue(undefined),
    cancelRollout: vi.fn().mockResolvedValue(undefined),
    retryFailedDevices: vi.fn().mockResolvedValue(undefined),
  } satisfies ReleaseChannelsApi;
}

const initialAuth = useFleetStore.getState().auth;
afterEach(() => useFleetStore.setState({ auth: initialAuth }));
beforeEach(() => {
  useFleetStore.setState({
    auth: { ...initialAuth, isAuthenticated: true, username: "operator", sessionGeneration: 1 },
  });
  vi.clearAllMocks();
  mockListFirmwareFiles.mockResolvedValue([]);
});

describe("delayed mutation selection", () => {
  const first = completedWithFailuresRigRollout;
  const other = { ...activeRigRollout, id: 102n, manufacturer: "Acme" };
  const successor = { ...activeRigRollout, id: 200n, assignmentGeneration: first.assignmentGeneration };

  function setup(initialKind: MonitorRequest["kind"] = "view") {
    const api = apiFor(first);
    api.rollouts = [first, other];
    const otherGroup = api.channels[0].modelGroups.find((group) => group.model === other.model)!;
    api.channels[0].modelGroups.push({
      ...otherGroup,
      manufacturer: other.manufacturer,
      assignmentGeneration: other.assignmentGeneration,
      firmwareChecksum: other.firmwareChecksum,
      activeRolloutId: other.id,
    });
    const handled = vi.fn();
    function Harness({ currentApi }: { currentApi: ReleaseChannelsApi }) {
      const [request, setRequest] = useState<MonitorRequest | null>({ kind: initialKind, rollout: first });
      return (
        <>
          <button onClick={() => setRequest({ kind: "view", rollout: first })}>View first history</button>
          <button onClick={() => setRequest({ kind: "view", rollout: other })}>View other history</button>
          <button onClick={() => setRequest({ kind: "rollback", rollout: first })}>Roll back first history</button>
          <button onClick={() => setRequest({ kind: "rollback", rollout: other })}>Roll back other history</button>
          <ActiveUpdatesMonitor
            api={currentApi}
            request={request}
            onManageChannel={vi.fn()}
            onRequestHandled={() => {
              handled();
              setRequest(null);
            }}
          />
        </>
      );
    }
    const result = render(<Harness currentApi={api} />);
    return {
      ...result,
      api,
      handled,
      refresh: (currentApi: ReleaseChannelsApi) => result.rerender(<Harness currentApi={currentApi} />),
    };
  }

  it("does not reopen detail when a retry finishes after it was closed", async () => {
    const pending = deferred<Rollout>();
    const { api, handled } = setup();
    api.retryFailedDevices.mockReturnValue(pending.promise);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
    expect(screen.queryByTestId("rollout-detail-header")).not.toBeInTheDocument();

    await act(async () => pending.resolve(successor));

    expect(screen.queryByTestId("rollout-detail-header")).not.toBeInTheDocument();
    expect(handled).toHaveBeenCalledOnce();
    expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(first.id, first.revision);
    expect(pushToast).toHaveBeenCalledWith({ message: expect.stringContaining("Retry requested"), status: "success" });
  });

  it("does not replace a reopened selection of the same rollout", async () => {
    const pending = deferred<Rollout>();
    const { api, handled } = setup();
    api.retryFailedDevices.mockReturnValue(pending.promise);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
    fireEvent.click(screen.getByRole("button", { name: "View first history" }));

    await act(async () => pending.resolve(successor));

    expect(screen.getByTestId(`rollout-detail-${first.id.toString()}`)).toBeInTheDocument();
    expect(screen.queryByTestId(`rollout-detail-${successor.id.toString()}`)).not.toBeInTheDocument();
    expect(handled).toHaveBeenCalledOnce();
  });

  it("does not replace another rollout opened from its banner", async () => {
    const pending = deferred<Rollout>();
    const { api, handled } = setup();
    api.retryFailedDevices.mockReturnValue(pending.promise);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
    fireEvent.click(within(screen.getByTestId(`update-banner-${other.id.toString()}`)).getByRole("button"));

    await act(async () => pending.resolve(successor));

    expect(screen.getByTestId(`rollout-detail-${other.id.toString()}`)).toBeInTheDocument();
    expect(screen.queryByTestId(`rollout-detail-${successor.id.toString()}`)).not.toBeInTheDocument();
    expect(handled).toHaveBeenCalledOnce();
  });

  it.each([
    { button: "View first history", selected: first },
    { button: "View other history", selected: other },
  ])("does not clear a newer external request from '$button'", async ({ button, selected }) => {
    const pending = deferred<Rollout>();
    const { api, handled } = setup();
    api.retryFailedDevices.mockReturnValue(pending.promise);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    fireEvent.click(screen.getByRole("button", { name: button }));
    if (selected.id !== first.id) expect(screen.getByTestId("view-rollout-retry-action")).not.toBeDisabled();

    await act(async () => pending.resolve(successor));

    expect(screen.getByTestId(`rollout-detail-${selected.id.toString()}`)).toBeInTheDocument();
    expect(screen.queryByTestId(`rollout-detail-${successor.id.toString()}`)).not.toBeInTheDocument();
    expect(handled).not.toHaveBeenCalled();
  });

  it.each([false, true])(
    "selects the successor while the same detail remains open (poll advances=%s)",
    async (advances) => {
      const pending = deferred<Rollout>();
      const { api, handled, refresh } = setup();
      api.retryFailedDevices.mockReturnValue(pending.promise);
      fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
      refresh({ ...api, rollouts: [{ ...first, revision: first.revision + (advances ? 1n : 0n) }, other] });

      await act(async () => pending.resolve(successor));

      expect(screen.getByTestId(`rollout-detail-${successor.id.toString()}`)).toBeInTheDocument();
      expect(handled).toHaveBeenCalledOnce();
      expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(first.id, first.revision);
    },
  );

  it("does not consume a request after its channel disappears", async () => {
    const pending = deferred<Rollout>();
    const { api, handled, refresh } = setup();
    api.retryFailedDevices.mockReturnValue(pending.promise);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    refresh({ ...api, channels: [], rollouts: [] });

    await act(async () => pending.resolve(successor));

    expect(screen.queryByTestId("rollout-detail-header")).not.toBeInTheDocument();
    expect(handled).not.toHaveBeenCalled();
  });

  it("does not consume an external request after the monitor unmounts", async () => {
    const pending = deferred<Rollout>();
    const { api, handled, unmount } = setup();
    api.retryFailedDevices.mockReturnValue(pending.promise);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    unmount();

    await act(async () => pending.resolve(successor));

    expect(handled).not.toHaveBeenCalled();
  });

  it.each([
    { button: "View other history", surface: "detail" },
    { button: "Roll back other history", surface: "rollback" },
    { button: "Roll back first history", surface: "rollback" },
  ])("does not let a delayed rollback replace '$button'", async ({ button, surface }) => {
    const pending = deferred<Rollout[]>();
    const { api, handled } = setup("rollback");
    api.rollbackFirmware.mockReturnValue(pending.promise);
    fireEvent.click(within(screen.getByTestId("rollback-firmware-dialog")).getByRole("button", { name: "Roll back" }));
    fireEvent.click(screen.getByRole("button", { name: button }));

    await act(async () => pending.resolve([successor]));

    if (surface === "detail") expect(screen.getByTestId(`rollout-detail-${other.id.toString()}`)).toBeInTheDocument();
    else expect(screen.getByTestId("rollback-firmware-dialog")).toBeInTheDocument();
    expect(screen.queryByTestId(`rollout-detail-${successor.id.toString()}`)).not.toBeInTheDocument();
    expect(handled).not.toHaveBeenCalled();
    expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(first);
  });

  it("opens a rollback successor when the same confirmation remains selected across polls", async () => {
    const pending = deferred<Rollout[]>();
    const { api, handled, refresh } = setup("rollback");
    api.rollbackFirmware.mockReturnValue(pending.promise);
    fireEvent.click(within(screen.getByTestId("rollback-firmware-dialog")).getByRole("button", { name: "Roll back" }));
    refresh({ ...api, rollouts: [{ ...first, revision: first.revision + 1n }, other] });

    await act(async () => pending.resolve([successor]));

    await waitFor(() => expect(screen.queryByTestId("rollback-firmware-dialog")).not.toBeInTheDocument());
    expect(screen.getByTestId(`rollout-detail-${successor.id.toString()}`)).toBeInTheDocument();
    expect(handled).toHaveBeenCalledOnce();
  });
});

describe("rollout controls use the operator's observed revision", () => {
  it.each([
    { action: "continue", method: "continueRollout", fixture: gatedRigRollout },
    { action: "pause", method: "pauseRollout", fixture: activeRigRollout },
    { action: "resume", method: "resumeRollout", fixture: pausedRigRollout },
    { action: "retry", method: "retryFailedDevices", fixture: completedWithFailuresRigRollout },
  ] as const)("sends the displayed revision for $action", async ({ action, method, fixture }) => {
    const observed = { ...fixture, revision: 7n };
    const api = apiFor(observed);
    const props = {
      api,
      request: { kind: "view" as const, rollout: observed },
      onManageChannel: vi.fn(),
    };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} />);

    fireEvent.click(screen.getByTestId(`view-rollout-${action}-action`));
    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [{ ...observed, revision: 8n }] }} />);

    await waitFor(() => expect(api[method]).toHaveBeenCalledExactlyOnceWith(observed.id, 7n));
    expect(pushToast).toHaveBeenCalledWith({
      message: expect.stringContaining(`${observed.manufacturer} ${observed.model}`),
      status: "success",
    });
  });

  it.each([
    { action: "cancel", method: "cancelRollout", dialog: "cancel-rollout-dialog", confirm: "Cancel remaining" },
    { action: "rollback", method: "rollbackFirmware", dialog: "rollback-firmware-dialog", confirm: "Roll back" },
  ] as const)(
    "keeps the confirmed $action revision after a newer poll and does not retry stale actions",
    async ({ action, method, dialog, confirm }) => {
      const observed = { ...activeRigRollout, revision: 7n };
      const api = apiFor(observed);
      const stale = new ConnectError("The update changed; review it again", Code.FailedPrecondition);
      api[method].mockRejectedValue(stale);
      const props = {
        api,
        request: { kind: "view" as const, rollout: observed },
        onManageChannel: vi.fn(),
      };
      const { rerender } = render(<ActiveUpdatesMonitor {...props} />);
      fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
      fireEvent.click(screen.getByTestId(`view-rollout-${action}-action`));
      expect(screen.getByTestId(dialog)).toBeInTheDocument();
      if (action === "cancel") {
        expect(screen.getByTestId(dialog)).toHaveTextContent("commands already sent may still finish");
        expect(screen.getByTestId(dialog)).not.toHaveTextContent("stops now");
      }

      rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [{ ...observed, revision: 8n }] }} />);
      fireEvent.click(within(screen.getByTestId(dialog)).getByRole("button", { name: confirm }));

      await waitFor(() => expect(pushToast).toHaveBeenCalledWith({ message: stale.message, status: "error" }));
      if (action === "rollback") expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(observed);
      else expect(api.cancelRollout).toHaveBeenCalledExactlyOnceWith(observed.id, 7n);
      expect(screen.getByTestId(dialog)).toBeInTheDocument();
    },
  );

  it("retains the history row through Firmware's rollback confirmation when a newer revision arrives", async () => {
    const observed = { ...activeRigRollout, revision: 7n };
    const api = apiFor(observed);
    mockUseReleaseChannels.mockReturnValue(api);
    const page = (
      <MemoryRouter initialEntries={["/settings/firmware?tab=release-channels"]}>
        <Firmware />
      </MemoryRouter>
    );
    const { rerender } = render(page);
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    fireEvent.click(screen.getByTestId("channel-history"));
    fireEvent.click(screen.getByTestId(`history-rollback-${observed.id.toString()}`));
    expect(screen.getByTestId("rollback-firmware-dialog")).toHaveTextContent(observed.previousFirmwareVersion);

    mockUseReleaseChannels.mockReturnValue({
      ...api,
      rollouts: [{ ...observed, revision: 8n, previousFirmwareVersion: "newer-target" }],
    });
    rerender(
      <MemoryRouter initialEntries={["/settings/firmware?tab=release-channels"]}>
        <Firmware />
      </MemoryRouter>,
    );
    const confirmation = screen.getByTestId("rollback-firmware-dialog");
    expect(confirmation).toHaveTextContent(observed.previousFirmwareVersion);
    expect(confirmation).not.toHaveTextContent("newer-target");
    fireEvent.click(within(confirmation).getByRole("button", { name: "Roll back" }));

    await waitFor(() => expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(observed));
  });

  it("explains clearing an empty prior assignment and reports the completed clear", async () => {
    const observed = { ...activeRigRollout, revision: 7n, previousFirmwareVersion: "" };
    const api = apiFor(observed);
    function Harness() {
      const [request, setRequest] = useState<MonitorRequest | null>({ kind: "rollback", rollout: observed });
      return (
        <ActiveUpdatesMonitor
          api={api}
          request={request}
          onRequestHandled={() => setRequest(null)}
          onManageChannel={vi.fn()}
        />
      );
    }
    render(<Harness />);
    const confirmation = screen.getByTestId("rollback-firmware-dialog");
    expect(confirmation).toHaveTextContent("Clear the firmware assignment?");
    expect(confirmation).toHaveTextContent("No firmware version will be enforced and no rollback update will start.");
    expect(confirmation).toHaveTextContent("update commands already sent may still finish.");
    expect(within(confirmation).queryByRole("button", { name: "Roll back" })).not.toBeInTheDocument();
    fireEvent.click(within(confirmation).getByRole("button", { name: "Clear assignment" }));

    await waitFor(() => expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(observed));
    expect(pushToast).toHaveBeenCalledWith({
      message: `Cleared the firmware assignment for ${observed.manufacturer} ${observed.model} in ${observed.channelName}`,
      status: "success",
    });
    await waitFor(() => expect(screen.queryByTestId("rollback-firmware-dialog")).not.toBeInTheDocument());
  });

  it("keeps the restore confirmation and success message for a nonempty prior assignment", async () => {
    const observed = activeRigRollout;
    const api = apiFor(observed);
    render(
      <ActiveUpdatesMonitor api={api} request={{ kind: "rollback", rollout: observed }} onManageChannel={vi.fn()} />,
    );
    const confirmation = screen.getByTestId("rollback-firmware-dialog");
    expect(confirmation).toHaveTextContent(`goes back to ${observed.previousFirmwareVersion}`);
    fireEvent.click(within(confirmation).getByRole("button", { name: "Roll back" }));

    await waitFor(() => expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(observed));
    expect(pushToast).toHaveBeenCalledWith({
      message: `Rolling ${observed.manufacturer} ${observed.model} in ${observed.channelName} back to ${observed.previousFirmwareVersion}`,
      status: "success",
    });
  });
});

describe("acknowledged rollback navigation", () => {
  const expectFinishedActions = () => {
    for (const action of ["continue", "pause", "resume", "retry"]) {
      expect(screen.queryByTestId(`view-rollout-${action}-action`)).not.toBeInTheDocument();
    }
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    expect(screen.queryByTestId("view-rollout-cancel-action")).not.toBeInTheDocument();
    expect(screen.queryByTestId("view-rollout-rollback-action")).not.toBeInTheDocument();
    expect(screen.getByTestId("view-rollout-view-miners-action")).toBeInTheDocument();
  };

  it("retires an applied assignment's retained detail and confirmation before channel groups refresh", async () => {
    const source = { ...gatedRigRollout, revision: 7n };
    const history = { ...completedRigRollout, id: source.id + 1n, assignmentGeneration: source.assignmentGeneration };
    const successor = {
      ...activeRigRollout,
      id: source.id + 100n,
      assignmentGeneration: source.assignmentGeneration + 1n,
      revision: 11n,
      firmwareVersion: "1.4.5",
      firmwareChecksum: "c".repeat(64),
    };
    const api = apiFor(source);
    function Harness({ currentApi }: { currentApi: ReleaseChannelsApi }) {
      const [request, setRequest] = useState<MonitorRequest | null>(null);
      return (
        <>
          <button onClick={() => setRequest({ kind: "view", rollout: { ...source, revision: source.revision + 1n } })}>
            Reopen cached source
          </button>
          <button onClick={() => setRequest({ kind: "view", rollout: history })}>View completed history</button>
          <button onClick={() => setRequest({ kind: "rollback", rollout: history })}>
            Roll back completed history
          </button>
          <ActiveUpdatesMonitor
            api={currentApi}
            request={request}
            onRequestHandled={() => setRequest(null)}
            onManageChannel={vi.fn()}
          />
        </>
      );
    }
    const { rerender } = render(<Harness currentApi={api} />);
    fireEvent.click(within(screen.getByTestId(`update-banner-${source.id}`)).getByRole("button"));
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("view-rollout-cancel-action"));
    expect(screen.getByTestId("cancel-rollout-dialog")).toBeInTheDocument();

    // Apply has returned a new generation, but its failed follow-up refresh
    // leaves the previous assignment and raw active source in the shared cache.
    rerender(<Harness currentApi={{ ...api, rollouts: [source, successor] }} />);
    expect(screen.queryByTestId(`update-banner-${source.id}`)).not.toBeInTheDocument();
    expect(screen.getByTestId(`update-banner-${successor.id}`)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByTestId("cancel-rollout-dialog")).not.toBeInTheDocument());
    expect(screen.getByTestId(`rollout-detail-${source.id}`)).toBeInTheDocument();
    expect(screen.getByTestId("rollout-status-headline")).toHaveTextContent("Superseded");
    expectFinishedActions();
    fireEvent.click(screen.getByRole("button", { name: "Close update details" }));

    fireEvent.click(screen.getByRole("button", { name: "Reopen cached source" }));
    expect(screen.getByTestId("rollout-status-headline")).toHaveTextContent("Superseded");
    expectFinishedActions();
    fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
    fireEvent.click(screen.getByRole("button", { name: "View completed history" }));
    expect(screen.getByTestId("rollout-status-headline")).toHaveTextContent("Completed");
    expectFinishedActions();
    fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
    fireEvent.click(screen.getByRole("button", { name: "Roll back completed history" }));
    expect(screen.queryByTestId("rollback-firmware-dialog")).not.toBeInTheDocument();

    fireEvent.click(within(screen.getByTestId(`update-banner-${successor.id}`)).getByRole("button"));
    fireEvent.click(screen.getByTestId("view-rollout-pause-action"));
    await waitFor(() => expect(api.pauseRollout).toHaveBeenCalledExactlyOnceWith(successor.id, successor.revision));
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("view-rollout-cancel-action"));
    fireEvent.click(
      within(screen.getByTestId("cancel-rollout-dialog")).getByRole("button", { name: "Cancel remaining" }),
    );
    await waitFor(() => expect(api.cancelRollout).toHaveBeenCalledExactlyOnceWith(successor.id, successor.revision));
    expect(api.rollbackFirmware).not.toHaveBeenCalled();
    expect(api.continueRollout).not.toHaveBeenCalled();
    expect(api.retryFailedDevices).not.toHaveBeenCalled();
  });

  it("cancels a returned successor with its captured revision while the assignment refresh is unavailable", async () => {
    const source = { ...activeRigRollout, revision: 7n };
    const successor = {
      ...activeRigRollout,
      id: source.id + 100n,
      assignmentGeneration: source.assignmentGeneration + 1n,
      revision: 11n,
      firmwareVersion: source.previousFirmwareVersion,
      firmwareChecksum: source.previousFirmwareChecksum,
    };
    const api = apiFor(source);
    const pending = deferred<Rollout[]>();
    api.rollbackFirmware.mockReturnValueOnce(pending.promise);
    const props = { onManageChannel: vi.fn() };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} api={api} />);
    fireEvent.click(within(screen.getByTestId(`update-banner-${source.id}`)).getByRole("button"));
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("view-rollout-rollback-action"));
    fireEvent.click(within(screen.getByTestId("rollback-firmware-dialog")).getByRole("button", { name: "Roll back" }));

    const acknowledgedApi = {
      ...api,
      acknowledgedRollbacks: [source],
      rollouts: [source, successor],
      channels: api.channels.map((channel) => ({
        ...channel,
        modelGroups: channel.modelGroups.map((group) =>
          group.manufacturer === source.manufacturer && group.model === source.model
            ? { ...group, rollbackPending: true }
            : group,
        ),
      })),
    };
    const refreshWarning = <div>Latest channel refresh failed</div>;
    rerender(<ActiveUpdatesMonitor {...props} api={acknowledgedApi} refreshWarning={refreshWarning} />);
    await act(async () => pending.resolve([successor]));
    expect(screen.getByTestId(`rollout-detail-${successor.id}`)).toBeInTheDocument();
    expect(screen.getByText("Latest channel refresh failed")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("view-rollout-cancel-action"));
    expect(screen.getByTestId("cancel-rollout-dialog")).toBeInTheDocument();

    // Polling may advance the rollout while its confirmation retains the
    // revision the operator selected and the channel assignment still lags.
    rerender(
      <ActiveUpdatesMonitor
        {...props}
        api={{ ...acknowledgedApi, rollouts: [source, { ...successor, revision: successor.revision + 1n }] }}
        refreshWarning={refreshWarning}
      />,
    );
    fireEvent.click(
      within(screen.getByTestId("cancel-rollout-dialog")).getByRole("button", { name: "Cancel remaining" }),
    );
    await waitFor(() => expect(api.cancelRollout).toHaveBeenCalledExactlyOnceWith(successor.id, successor.revision));
  });

  it.each(["cancel", "rollback"] as const)(
    "does not restore a retained %s confirmation when its acknowledgment retires after navigation",
    async (action) => {
      const source = { ...activeRigRollout, revision: 7n };
      const other = { ...activeRigRollout, id: source.id + 100n, channelId: 2n, channelName: "Production" };
      const api = apiFor(source);
      api.rollouts = [source, other];
      api.channels.push({ ...apiFor(other).channels[0], id: other.channelId, name: other.channelName });
      const canceled = deferred();
      const rolledBack = deferred<Rollout[]>();
      api.cancelRollout.mockReturnValueOnce(canceled.promise);
      api.rollbackFirmware.mockReturnValueOnce(rolledBack.promise);
      function Harness({ currentApi }: { currentApi: ReleaseChannelsApi }) {
        const [request, setRequest] = useState<MonitorRequest | null>(null);
        return (
          <>
            <button onClick={() => setRequest({ kind: "view", rollout: other })}>View other history</button>
            <button onClick={() => setRequest({ kind: "rollback", rollout: source })}>Reopen old rollback</button>
            <ActiveUpdatesMonitor
              api={currentApi}
              request={request}
              onRequestHandled={() => setRequest(null)}
              onManageChannel={vi.fn()}
            />
          </>
        );
      }
      const { rerender } = render(<Harness currentApi={api} />);
      fireEvent.click(within(screen.getByTestId(`update-banner-${source.id}`)).getByRole("button"));
      fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
      fireEvent.click(screen.getByTestId(`view-rollout-${action}-action`));
      const dialog = action === "cancel" ? "cancel-rollout-dialog" : "rollback-firmware-dialog";
      fireEvent.click(
        within(screen.getByTestId(dialog)).getByRole("button", {
          name: action === "cancel" ? "Cancel remaining" : "Roll back",
        }),
      );
      rerender(<Harness currentApi={{ ...api, acknowledgedRollbacks: [source] }} />);
      await waitFor(() => expect(screen.queryByTestId(dialog)).not.toBeInTheDocument());
      fireEvent.click(screen.getByRole("button", { name: "View other history" }));
      await act(async () => {
        if (action === "cancel") canceled.resolve();
        else rolledBack.resolve([]);
      });
      expect(screen.getByTestId(`rollout-detail-${other.id}`)).toBeInTheDocument();

      // A complete read advances the assignment and retires its acknowledgment.
      // The old confirmation remains captured because navigation superseded its response.
      const refreshed = {
        ...api,
        acknowledgedRollbacks: [],
        channels: api.channels.map((channel) =>
          channel.id === source.channelId
            ? {
                ...channel,
                modelGroups: channel.modelGroups.map((group) =>
                  group.manufacturer === source.manufacturer && group.model === source.model
                    ? {
                        ...group,
                        assignmentGeneration: source.assignmentGeneration + 1n,
                        activeRolloutId: 0n,
                        firmwareVersion: source.previousFirmwareVersion,
                        firmwareChecksum: source.previousFirmwareChecksum,
                        firmwareFileId: "fw-rig-143",
                      }
                    : group,
                ),
              }
            : channel,
        ),
        rollouts: [other],
      };
      rerender(<Harness currentApi={refreshed} />);
      expect(screen.queryByTestId(dialog)).not.toBeInTheDocument();
      expect(screen.getByTestId(`rollout-detail-${other.id}`)).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
      expect(screen.queryByTestId(dialog)).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Reopen old rollback" }));
      expect(screen.queryByTestId("rollback-firmware-dialog")).not.toBeInTheDocument();
      if (action === "cancel") {
        expect(api.cancelRollout).toHaveBeenCalledExactlyOnceWith(source.id, source.revision);
        expect(api.rollbackFirmware).not.toHaveBeenCalled();
      } else {
        expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(source);
        expect(api.cancelRollout).not.toHaveBeenCalled();
      }
    },
  );

  it.each(["", "1.4.3"])(
    "retires captured source details after rollback acknowledgment while preserving navigation (previous=%j)",
    async (previousFirmwareVersion) => {
      const source = { ...gatedRigRollout, revision: 7n, previousFirmwareVersion };
      const successor = {
        ...activeRigRollout,
        id: source.id + 100n,
        assignmentGeneration: source.assignmentGeneration + 1n,
        firmwareVersion: previousFirmwareVersion,
      };
      const api = apiFor(source);
      const pending = deferred<Rollout[]>();
      api.rollbackFirmware.mockReturnValueOnce(pending.promise);
      function Harness({ currentApi }: { currentApi: ReleaseChannelsApi }) {
        const [request, setRequest] = useState<MonitorRequest | null>(null);
        return (
          <>
            <button onClick={() => setRequest({ kind: "view", rollout: source })}>Reopen source history</button>
            <button onClick={() => setRequest({ kind: "rollback", rollout: source })}>Request source rollback</button>
            <ActiveUpdatesMonitor
              api={currentApi}
              request={request}
              onRequestHandled={() => setRequest(null)}
              onManageChannel={vi.fn()}
            />
          </>
        );
      }
      const { rerender } = render(<Harness currentApi={api} />);
      fireEvent.click(within(screen.getByTestId(`update-banner-${source.id}`)).getByRole("button"));
      fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
      fireEvent.click(screen.getByTestId("view-rollout-rollback-action"));
      fireEvent.click(
        within(screen.getByTestId("rollback-firmware-dialog")).getByRole("button", {
          name: previousFirmwareVersion ? "Roll back" : "Clear assignment",
        }),
      );

      const started = previousFirmwareVersion ? [successor] : [];
      // The hook publishes the committed acknowledgment before a failed
      // follow-up read releases the initiating mutation promise.
      rerender(<Harness currentApi={{ ...api, acknowledgedRollbacks: [source], rollouts: [source, ...started] }} />);
      expect(screen.queryByTestId(`update-banner-${source.id}`)).not.toBeInTheDocument();
      await act(async () => pending.resolve(started));
      expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(source);
      await waitFor(() => expect(screen.queryByTestId("rollback-firmware-dialog")).not.toBeInTheDocument());
      if (previousFirmwareVersion) {
        expect(screen.getByTestId(`rollout-detail-${successor.id}`)).toBeInTheDocument();
        expect(screen.getByTestId(`update-banner-${successor.id}`)).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
      } else {
        expect(screen.queryByTestId("rollout-detail-header")).not.toBeInTheDocument();
      }

      fireEvent.click(screen.getByRole("button", { name: "Reopen source history" }));
      expect(screen.getByTestId("rollout-status-headline")).toHaveTextContent("Rolled back");
      expectFinishedActions();
      fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
      fireEvent.click(screen.getByRole("button", { name: "Request source rollback" }));
      expect(screen.queryByTestId("rollback-firmware-dialog")).not.toBeInTheDocument();
      expect(api.rollbackFirmware).toHaveBeenCalledOnce();
      expect(api.continueRollout).not.toHaveBeenCalled();
      expect(api.pauseRollout).not.toHaveBeenCalled();
      expect(api.retryFailedDevices).not.toHaveBeenCalled();
    },
  );

  it("retires a historical source's reconciliation run without changing later assignments or other pairs", () => {
    const source = { ...completedRigRollout, assignmentGeneration: 5n, revision: 7n };
    const reconciling = {
      ...activeRigRollout,
      id: source.id + 100n,
      assignmentGeneration: source.assignmentGeneration,
      manufacturer: " proto ",
      model: " rig ",
      revision: 9n,
    };
    const older = { ...reconciling, id: reconciling.id + 1n, assignmentGeneration: 4n };
    const unrelated = { ...reconciling, id: reconciling.id + 2n, manufacturer: "Acme" };
    const successor = { ...activeRigRollout, id: reconciling.id + 3n, assignmentGeneration: 6n };
    const api = {
      ...apiFor(source),
      acknowledgedRollbacks: [source],
      rollouts: [reconciling, older, unrelated, successor],
    };
    const props = { api, request: { kind: "view" as const, rollout: source }, onManageChannel: vi.fn() };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} />);
    expect(screen.getByTestId("rollout-status-headline")).toHaveTextContent("Completed");
    expectFinishedActions();
    expect(screen.queryByTestId(`update-banner-${reconciling.id}`)).not.toBeInTheDocument();
    expect(screen.queryByTestId(`update-banner-${older.id}`)).not.toBeInTheDocument();
    expect(screen.getByTestId(`update-banner-${unrelated.id}`)).toBeInTheDocument();
    expect(screen.getByTestId(`update-banner-${successor.id}`)).toBeInTheDocument();

    rerender(<ActiveUpdatesMonitor {...props} request={{ kind: "view", rollout: reconciling }} />);
    expect(screen.getByTestId("rollout-status-headline")).toHaveTextContent("Rolled back");
    expectFinishedActions();

    rerender(
      <ActiveUpdatesMonitor
        {...props}
        api={{ ...api, channels: apiFor(successor).channels }}
        request={{ kind: "view", rollout: successor }}
      />,
    );
    expect(screen.getByTestId("view-rollout-pause-action")).toBeInTheDocument();
    expect(screen.getByTestId("view-rollout-retry-action")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    expect(screen.getByTestId("view-rollout-cancel-action")).toBeInTheDocument();
    expect(screen.getByTestId("view-rollout-rollback-action")).toBeInTheDocument();
  });

  it("keeps acknowledged history terminal when a cached active source is reopened from the Firmware page", async () => {
    const source = { ...activeRigRollout, revision: 7n };
    const api = apiFor(source);
    mockUseReleaseChannels.mockReturnValue(api);
    const page = () => (
      <MemoryRouter initialEntries={["/settings/firmware?tab=release-channels"]}>
        <Firmware />
      </MemoryRouter>
    );
    const { rerender } = render(page());
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    fireEvent.click(screen.getByTestId("channel-history"));
    fireEvent.click(await screen.findByTestId(`history-view-${source.id}`));
    fireEvent.click(screen.getByRole("button", { name: "Close update details" }));

    mockUseReleaseChannels.mockReturnValue({ ...api, acknowledgedRollbacks: [source], rollouts: [] });
    rerender(page());
    fireEvent.click(screen.getByTestId("channel-history"));
    const row = await screen.findByTestId(`history-row-${source.id}`);
    expect(row).toHaveTextContent("Rolled back");
    expect(within(row).queryByRole("button", { name: /^Roll back/ })).not.toBeInTheDocument();
    fireEvent.click(within(row).getByRole("button", { name: "View" }));
    expect(screen.getByTestId("rollout-status-headline")).toHaveTextContent("Rolled back");
    expectFinishedActions();
    expect(api.rollbackFirmware).not.toHaveBeenCalled();
  });
});

describe("rollout manufacturer identity", () => {
  it.each([
    { action: "cancel", method: "cancelRollout", dialog: "cancel-rollout-dialog", confirm: "Cancel remaining" },
    { action: "rollback", method: "rollbackFirmware", dialog: "rollback-firmware-dialog", confirm: "Roll back" },
  ] as const)("keeps a same-named model identifiable through $action", async ({ action, dialog, confirm }) => {
    const first = activeRigRollout;
    const second = { ...first, id: first.id + 1n, manufacturer: "Acme", revision: 7n };
    const api = apiFor(first);
    const firstGroup = api.channels[0].modelGroups.find((group) => group.model === first.model)!;
    api.channels[0].modelGroups.push({ ...firstGroup, manufacturer: second.manufacturer, activeRolloutId: second.id });
    api.rollouts = [first, second];
    render(<ActiveUpdatesMonitor api={api} onManageChannel={vi.fn()} />);

    expect(screen.getByTestId(`update-banner-${first.id.toString()}`)).toHaveTextContent(
      "Canary, Proto Rig firmware update",
    );
    const secondBanner = screen.getByTestId(`update-banner-${second.id.toString()}`);
    expect(secondBanner).toHaveTextContent("Canary, Acme Rig firmware update");
    fireEvent.click(within(secondBanner).getByRole("button", { name: "View update" }));
    expect(screen.getByTestId("rollout-detail-header")).toHaveTextContent("Canary, Acme Rig firmware update");
    expect(screen.getByTestId("rollout-detail-header")).not.toHaveTextContent("Proto Rig");
    fireEvent.click(screen.getByRole("button", { name: "More actions for Canary, Acme Rig firmware update" }));
    fireEvent.click(screen.getByTestId(`view-rollout-${action}-action`));

    const confirmation = screen.getByTestId(dialog);
    expect(confirmation).toHaveTextContent("Acme Rig");
    expect(confirmation).not.toHaveTextContent("Proto Rig");
    fireEvent.click(within(confirmation).getByRole("button", { name: confirm }));
    await waitFor(() => {
      if (action === "rollback") expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(second);
      else expect(api.cancelRollout).toHaveBeenCalledExactlyOnceWith(second.id, 7n);
    });
    expect(pushToast).toHaveBeenCalledWith({ message: expect.stringContaining("Acme Rig"), status: "success" });
  });
});

describe("on-demand history detail handoff", () => {
  it.each([
    { kind: "view", callback: true },
    { kind: "rollback", callback: true },
    { kind: "view", callback: false },
    { kind: "rollback", callback: false },
  ] as const)(
    "keeps banners usable after deleting an external $kind request's channel (callback=$callback)",
    async ({ kind, callback }) => {
      const observed = completedWithFailuresRigRollout;
      const survivor = { ...activeRigRollout, id: 101n, channelId: 2n, channelName: "Production" };
      const api = apiFor(observed);
      const survivorChannel = { ...apiFor(survivor).channels[0], id: survivor.channelId, name: survivor.channelName };
      const handled = vi.fn();
      function Harness({ channels }: { channels: ReleaseChannelsApi["channels"] }) {
        const [request, setRequest] = useState<MonitorRequest | null>({ kind, rollout: observed });
        return (
          <ActiveUpdatesMonitor
            api={{ ...api, channels, rollouts: [survivor] }}
            request={request}
            onRequestHandled={
              callback
                ? () => {
                    handled();
                    setRequest(null);
                  }
                : undefined
            }
            onManageChannel={vi.fn()}
          />
        );
      }
      const { rerender } = render(<Harness channels={[...api.channels, survivorChannel]} />);
      const surface = kind === "view" ? "rollout-detail-header" : "rollback-firmware-dialog";
      expect(screen.getByTestId(surface)).toBeInTheDocument();

      rerender(<Harness channels={[survivorChannel]} />);
      await waitFor(() => expect(screen.queryByTestId(surface)).not.toBeInTheDocument());
      fireEvent.click(
        within(screen.getByTestId(`update-banner-${survivor.id.toString()}`)).getByRole("button", {
          name: "View update",
        }),
      );
      expect(screen.getByTestId("rollout-detail-header")).toHaveTextContent("Production, Proto Rig firmware update");
      expect(screen.getByTestId(`rollout-detail-${survivor.id.toString()}`)).toBeInTheDocument();
      expect(handled).toHaveBeenCalledTimes(callback ? 1 : 0);
      expect(api.rollbackFirmware).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "Close update details" }));
      await waitFor(() => expect(screen.queryByTestId("rollout-detail-header")).not.toBeInTheDocument());
    },
  );

  it.each(["cancel", "rollback"] as const)(
    "dismisses a local %s confirmation when its channel disappears",
    async (action) => {
      const observed = activeRigRollout;
      const api = apiFor(observed);
      const { rerender } = render(<ActiveUpdatesMonitor api={api} onManageChannel={vi.fn()} />);
      fireEvent.click(
        within(screen.getByTestId(`update-banner-${observed.id.toString()}`)).getByRole("button", {
          name: "View update",
        }),
      );
      fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
      fireEvent.click(screen.getByTestId(`view-rollout-${action}-action`));
      const dialog = action === "cancel" ? "cancel-rollout-dialog" : "rollback-firmware-dialog";
      expect(screen.getByTestId(dialog)).toBeInTheDocument();

      rerender(<ActiveUpdatesMonitor api={{ ...api, channels: [], rollouts: [] }} onManageChannel={vi.fn()} />);
      await waitFor(() => expect(screen.queryByTestId(dialog)).not.toBeInTheDocument());
      expect(screen.queryByTestId("rollout-detail-header")).not.toBeInTheDocument();
      expect(api.cancelRollout).not.toHaveBeenCalled();
      expect(api.rollbackFirmware).not.toHaveBeenCalled();
    },
  );

  it.each([
    { reason: "canceled remaining work", fixture: canceledRemainingRigRollout },
    {
      reason: "skipped miners",
      fixture: { ...completedRigRollout, deviceCounts: create(RolloutDeviceCountsSchema, { skipped: 6 }) },
    },
    { reason: "stopped miners from earlier updates", fixture: completedRigRollout },
  ])("offers retry for $reason without claiming an update necessarily started", async ({ fixture }) => {
    const observed = { ...fixture, revision: 7n };
    const api = apiFor(observed);
    api.retryFailedDevices.mockResolvedValue(observed);
    render(<ActiveUpdatesMonitor api={api} request={{ kind: "view", rollout: observed }} onManageChannel={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Retry remaining" }));
    await waitFor(() => expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(observed.id, 7n));
    expect(pushToast).toHaveBeenCalledWith({
      message: `Retry requested for remaining ${observed.manufacturer} ${observed.model} miners in ${observed.channelName}`,
      status: "success",
    });
    expect(screen.getByTestId(`rollout-detail-${observed.id.toString()}`)).toBeInTheDocument();
  });

  it("updates terminal retry eligibility when another active update appears or the assignment changes", () => {
    const observed = completedWithFailuresRigRollout;
    const api = apiFor(observed);
    const props = { request: { kind: "view" as const, rollout: observed }, onManageChannel: vi.fn() };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} api={api} />);
    expect(screen.getByTestId("view-rollout-retry-action")).toBeInTheDocument();

    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [observed, activeRigRollout] }} />);
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();

    rerender(<ActiveUpdatesMonitor {...props} api={api} />);
    expect(screen.getByTestId("view-rollout-retry-action")).toBeInTheDocument();
    rerender(
      <ActiveUpdatesMonitor
        {...props}
        api={{
          ...api,
          channels: api.channels.map((channel) => ({
            ...channel,
            modelGroups: channel.modelGroups.map((group) => ({
              ...group,
              assignmentGeneration: group.assignmentGeneration + 1n,
            })),
          })),
        }}
      />,
    );
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();
    expect(api.retryFailedDevices).not.toHaveBeenCalled();
  });

  it("opens a historical update absent from the polling baseline, then follows a newer live revision", async () => {
    const observed = { ...completedWithFailuresRigRollout, revision: 7n };
    const api = apiFor(observed);
    mockUseReleaseChannels.mockReturnValue({ ...api, rollouts: [] });
    const page = () => (
      <MemoryRouter initialEntries={["/settings/firmware?tab=release-channels"]}>
        <Firmware />
      </MemoryRouter>
    );
    const { rerender } = render(page());
    expect(api.listChannelRollouts).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    fireEvent.click(screen.getByTestId("channel-history"));
    fireEvent.click(await screen.findByTestId(`history-view-${observed.id.toString()}`));
    expect(screen.getByTestId("view-rollout-retry-action")).toBeInTheDocument();
    expect(api.listChannelRollouts).toHaveBeenCalledExactlyOnceWith(observed.channelId, expect.any(AbortSignal));
    mockUseReleaseChannels.mockReturnValue({ ...api, rollouts: [{ ...observed, revision: 8n }] });
    rerender(page());
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    await waitFor(() => expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(observed.id, 8n));
  });

  it("opens the returned successor when its follow-up refresh has not loaded it", async () => {
    const observed = completedWithFailuresRigRollout;
    const successor = { ...activeRigRollout, id: observed.id + 1n, model: "Successor model" };
    const api = apiFor(observed);
    api.retryFailedDevices.mockResolvedValue(successor);
    function Harness() {
      const [request, setRequest] = useState<MonitorRequest | null>({ kind: "view", rollout: observed });
      return (
        <ActiveUpdatesMonitor
          api={{ ...api, rollouts: [] }}
          request={request}
          onManageChannel={vi.fn()}
          onRequestHandled={() => setRequest(null)}
        />
      );
    }
    render(<Harness />);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    await waitFor(() => expect(screen.getByTestId(`rollout-detail-${successor.id.toString()}`)).toBeInTheDocument());
    expect(screen.getByTestId("rollout-detail-header")).toHaveTextContent("Successor model");
  });

  it("keeps a newer historical revision and drops its fallback when the channel disappears", async () => {
    const observed = { ...completedWithFailuresRigRollout, revision: 7n };
    const api = apiFor(observed);
    const props = {
      api: { ...api, rollouts: [{ ...observed, revision: 6n }] },
      request: { kind: "view" as const, rollout: observed },
      onManageChannel: vi.fn(),
    };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} />);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    await waitFor(() => expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(observed.id, 7n));
    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, channels: [], rollouts: [] }} />);
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();
  });
});
