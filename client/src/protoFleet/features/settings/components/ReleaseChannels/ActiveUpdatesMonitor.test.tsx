import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";

import ActiveUpdatesMonitor from "./ActiveUpdatesMonitor";
import {
  activeRigRollout,
  canaryChannel,
  canaryPreview,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import Firmware from "@/protoFleet/features/settings/components/Firmware";
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
        modelGroups: canaryChannel.modelGroups.map((group) => ({
          ...group,
          assignmentGeneration: rollout.assignmentGeneration,
        })),
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

beforeEach(() => {
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
      request: { kind: "view" as const, rolloutId: observed.id },
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
        request: { kind: "view" as const, rolloutId: observed.id },
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
});
