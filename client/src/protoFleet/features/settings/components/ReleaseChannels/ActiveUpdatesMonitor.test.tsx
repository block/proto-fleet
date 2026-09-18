import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";

import ActiveUpdatesMonitor, { type MonitorRequest } from "./ActiveUpdatesMonitor";
import {
  activeRigRollout,
  canaryChannel,
  canaryPreview,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import { isActive } from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
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

      rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [{ ...observed, revision: 8n }] }} />);
      fireEvent.click(within(screen.getByTestId(dialog)).getByRole("button", { name: confirm }));

      await waitFor(() => expect(pushToast).toHaveBeenCalledWith({ message: stale.message, status: "error" }));
      expect(api[method]).toHaveBeenCalledExactlyOnceWith(observed.id, 7n);
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

    await waitFor(() => expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(observed.id, 7n));
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

    await waitFor(() => expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(observed.id, 7n));
    expect(pushToast).toHaveBeenCalledWith({
      message: `Cleared the firmware assignment for ${observed.model} in ${observed.channelName}`,
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

    await waitFor(() => expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(observed.id, observed.revision));
    expect(pushToast).toHaveBeenCalledWith({
      message: `Rolling ${observed.model} in ${observed.channelName} back to ${observed.previousFirmwareVersion}`,
      status: "success",
    });
  });
});

describe("on-demand history detail handoff", () => {
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
    await waitFor(() => expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument());
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
