import { useState } from "react";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred } from "./__tests__/helpers";
import { apiFor, apiWithBanners } from "./__tests__/monitorHelpers";
import ActiveUpdatesMonitor, { type MonitorRequest } from "./ActiveUpdatesMonitor";
import {
  activeRigRollout,
  canceledRemainingRigRollout,
  completedRigRollout,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import { useFleetStore } from "@/protoFleet/store";

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

const initialAuth = useFleetStore.getState().auth;
afterEach(() => useFleetStore.setState({ auth: initialAuth }));
beforeEach(() => {
  useFleetStore.setState({
    auth: { ...initialAuth, isAuthenticated: true, username: "operator", sessionGeneration: 1 },
  });
  vi.clearAllMocks();
});

describe("single active update on the firmware page", () => {
  it("shows live controls inline and only reveals metrics when View details is expanded", () => {
    const api = apiFor(gatedRigRollout);
    render(<ActiveUpdatesMonitor api={api} onManageChannel={vi.fn()} />);

    const card = within(screen.getByTestId("inline-rollout-live-view"));
    expect(screen.getByTestId(`active-update-${gatedRigRollout.id}`)).toBeInTheDocument();
    expect(screen.getByTestId("inline-rollout-identifier")).toHaveTextContent("Proto Rig");
    expect(card.getByTestId("inline-rollout-status-headline")).toBeInTheDocument();
    expect(card.getByTestId("inline-rollout-detail-progress")).toBeInTheDocument();
    expect(card.getByRole("button", { name: "Continue" })).toBeInTheDocument();
    expect(card.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(card.queryByTestId("inline-rollout-detail-stats")).not.toBeInTheDocument();
    expect(card.queryByTestId("inline-rollout-performance")).not.toBeInTheDocument();
    expect(screen.queryByTestId(`update-banner-${gatedRigRollout.id}`)).not.toBeInTheDocument();
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    const toggle = card.getByRole("button", { name: "View details" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(toggle);
    expect(card.getByRole("button", { name: "Hide details" })).toHaveAttribute("aria-expanded", "true");
    expect(card.getByTestId("inline-rollout-detail-stats")).toBeInTheDocument();
    expect(card.getByTestId("inline-rollout-performance")).toBeInTheDocument();
    fireEvent.click(card.getByRole("button", { name: "Hide details" }));
    expect(card.queryByTestId("inline-rollout-detail-stats")).not.toBeInTheDocument();
    expect(card.queryByTestId("inline-rollout-performance")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    for (const method of [
      "refresh",
      "listRolloutDevices",
      "continueRollout",
      "pauseRollout",
      "resumeRollout",
      "cancelRollout",
      "rollbackFirmware",
      "retryFailedDevices",
    ] as const) {
      expect(api[method]).not.toHaveBeenCalled();
    }
  });

  it.each([
    { action: "continue", method: "continueRollout", fixture: gatedRigRollout },
    { action: "pause", method: "pauseRollout", fixture: activeRigRollout },
    { action: "resume", method: "resumeRollout", fixture: pausedRigRollout },
  ] as const)("uses the inline card's observed revision for $action", async ({ action, method, fixture }) => {
    const observed = { ...fixture, revision: 7n };
    const api = apiFor(observed);
    const props = { api, onManageChannel: vi.fn() };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} />);
    fireEvent.click(screen.getByTestId(`inline-view-rollout-${action}-action`));
    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [{ ...observed, revision: 8n }] }} />);

    await waitFor(() => expect(api[method]).toHaveBeenCalledExactlyOnceWith(observed.id, 7n));
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
  });

  it.each([
    { action: "cancel", dialog: "cancel-rollout-dialog", confirm: "Cancel remaining" },
    { action: "rollback", dialog: "rollback-firmware-dialog", confirm: "Roll back" },
  ] as const)(
    "retains the inline $action confirmation snapshot through a poll",
    async ({ action, dialog, confirm }) => {
      const observed = { ...activeRigRollout, revision: 7n };
      const api = apiFor(observed);
      const props = { api, onManageChannel: vi.fn() };
      const { rerender } = render(<ActiveUpdatesMonitor {...props} />);
      fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
      fireEvent.click(screen.getByTestId(`inline-view-rollout-${action}-action`));
      expect(screen.getByTestId(dialog)).toBeInTheDocument();
      rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [{ ...observed, revision: 8n }] }} />);
      fireEvent.click(within(screen.getByTestId(dialog)).getByRole("button", { name: confirm }));

      await waitFor(() => {
        if (action === "cancel") expect(api.cancelRollout).toHaveBeenCalledExactlyOnceWith(observed.id, 7n);
        else expect(api.rollbackFirmware).toHaveBeenCalledExactlyOnceWith(observed);
      });
    },
  );

  it("switches from one inline update to banners and back without routing actions to a removed rollout", async () => {
    const first = { ...activeRigRollout, revision: 7n };
    const second = { ...pausedRigRollout, id: 102n, manufacturer: "Acme", revision: 11n };
    const api = apiFor(first);
    const secondGroup = apiFor(second).channels[0].modelGroups.find((group) => group.model === second.model)!;
    api.channels[0].modelGroups.push({ ...secondGroup, manufacturer: second.manufacturer });
    const props = { onManageChannel: vi.fn() };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} api={api} />);
    fireEvent.click(screen.getByTestId("inline-view-rollout-pause-action"));
    await waitFor(() => expect(api.pauseRollout).toHaveBeenCalledExactlyOnceWith(first.id, 7n));

    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [first, second] }} />);
    expect(screen.queryByTestId("inline-rollout-live-view")).not.toBeInTheDocument();
    expect(screen.getByTestId(`update-banner-${first.id}`)).toBeInTheDocument();
    fireEvent.click(within(screen.getByTestId(`update-banner-${second.id}`)).getByRole("button"));
    expect(screen.getByTestId("rollout-detail-title")).toHaveTextContent("Acme Rig");
    fireEvent.click(screen.getByRole("button", { name: "Back" }));

    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [second] }} />);
    expect(screen.queryByTestId(`active-update-${first.id}`)).not.toBeInTheDocument();
    expect(screen.getByTestId("inline-rollout-identifier")).toHaveTextContent("Acme Rig");
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("inline-view-rollout-resume-action"));
    await waitFor(() => expect(api.resumeRollout).toHaveBeenCalledExactlyOnceWith(second.id, 11n));
    expect(api.pauseRollout).toHaveBeenCalledOnce();
  });

  it("keeps an in-flight action locked across inline and fullscreen layout changes", async () => {
    const observed = { ...activeRigRollout, revision: 7n };
    const api = apiFor(observed);
    const pending = deferred();
    api.pauseRollout.mockReturnValueOnce(pending.promise);
    const other = { ...observed, id: 102n, manufacturer: "Acme" };
    const props = { onManageChannel: vi.fn() };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} api={api} />);
    fireEvent.click(screen.getByTestId("inline-view-rollout-pause-action"));

    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [observed, other] }} />);
    fireEvent.click(within(screen.getByTestId(`update-banner-${observed.id}`)).getByRole("button"));
    expect(screen.getByTestId("view-rollout-pause-action")).toBeDisabled();
    fireEvent.click(screen.getByTestId("view-rollout-pause-action"));
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    rerender(<ActiveUpdatesMonitor {...props} api={api} />);
    expect(screen.getByTestId("inline-view-rollout-pause-action")).toBeDisabled();
    fireEvent.click(screen.getByTestId("inline-view-rollout-pause-action"));
    expect(api.pauseRollout).toHaveBeenCalledExactlyOnceWith(observed.id, 7n);

    await act(async () => pending.resolve());
    expect(screen.getByTestId("inline-view-rollout-pause-action")).toBeEnabled();
  });

  it("captures the inline retry revision before a newer poll arrives", async () => {
    const observed = { ...activeRigRollout, revision: 7n };
    const api = apiFor(observed);
    const props = { onManageChannel: vi.fn() };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} api={api} />);
    fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("inline-view-rollout-retry-action"));
    expect(screen.getByTestId("retry-rollout-dialog")).toBeInTheDocument();
    expect(api.retryFailedDevices).not.toHaveBeenCalled();

    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [{ ...observed, revision: 8n }] }} />);
    fireEvent.click(screen.getByTestId("confirm-rollout-retry"));
    await waitFor(() => expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(observed.id, 7n));
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
  });

  it.each(["confirm", "cancel"])(
    "keeps the inline retry confirmation on its original update when a second update starts (%s)",
    async (action) => {
      const observed = { ...activeRigRollout, revision: 7n };
      const api = apiWithBanners(observed);
      const second = api.rollouts[1]!;
      const props = { onManageChannel: vi.fn() };
      const { rerender } = render(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [observed] }} />);
      fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
      fireEvent.click(screen.getByTestId("inline-view-rollout-retry-action"));
      expect(screen.getByTestId("retry-rollout-dialog")).toHaveTextContent("Proto Rig in Canary");

      rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [{ ...observed, revision: 8n }, second] }} />);
      const confirmation = screen.getByTestId("retry-rollout-dialog");
      expect(confirmation).toHaveTextContent("Proto Rig in Canary");
      expect(confirmation).not.toHaveTextContent("Other Rig");
      expect(api.retryFailedDevices).not.toHaveBeenCalled();
      expect(screen.queryByTestId("inline-rollout-live-view")).not.toBeInTheDocument();
      if (action === "confirm") {
        fireEvent.click(within(confirmation).getByTestId("confirm-rollout-retry"));
        await waitFor(() => expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(observed.id, 7n));
      } else {
        fireEvent.click(within(confirmation).getByRole("button", { name: "Cancel" }));
        expect(api.retryFailedDevices).not.toHaveBeenCalled();
      }
      await waitFor(() => expect(screen.queryByTestId("retry-rollout-dialog")).not.toBeInTheDocument());
      expect(screen.getByTestId(`update-banner-${observed.id}`)).toBeInTheDocument();
      expect(screen.getByTestId(`update-banner-${second.id}`)).toBeInTheDocument();
      expect(screen.queryByTestId("inline-rollout-live-view")).not.toBeInTheDocument();
      expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
    },
  );

  it.each(["channel deletion", "assignment replacement"])(
    "dismisses a retained inline retry confirmation after %s",
    async (reason) => {
      const observed = { ...activeRigRollout, revision: 7n };
      const api = apiWithBanners(observed);
      const props = { onManageChannel: vi.fn() };
      const { rerender } = render(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [observed] }} />);
      fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
      fireEvent.click(screen.getByTestId("inline-view-rollout-retry-action"));
      rerender(<ActiveUpdatesMonitor {...props} api={api} />);
      expect(screen.getByTestId("retry-rollout-dialog")).toBeInTheDocument();

      const invalidatedApi =
        reason === "channel deletion"
          ? { ...api, channels: [], rollouts: [] }
          : {
              ...api,
              channels: api.channels.map((channel) => ({
                ...channel,
                modelGroups: channel.modelGroups.map((group) =>
                  group.manufacturer === observed.manufacturer && group.model === observed.model
                    ? { ...group, assignmentGeneration: observed.assignmentGeneration + 1n, activeRolloutId: 0n }
                    : group,
                ),
              })),
            };
      rerender(<ActiveUpdatesMonitor {...props} api={invalidatedApi} />);
      await waitFor(() => expect(screen.queryByTestId("retry-rollout-dialog")).not.toBeInTheDocument());
      expect(api.retryFailedDevices).not.toHaveBeenCalled();
      expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
    },
  );

  it("keeps the inline miner drilldown and its pending read on the original update when another starts", async () => {
    const observed = { ...activeRigRollout, revision: 7n };
    const pending = deferred<[]>();
    const listRolloutDevices = vi
      .fn<ReleaseChannelsApi["listRolloutDevices"]>()
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValue([]);
    const api = { ...apiWithBanners(observed), listRolloutDevices };
    const second = api.rollouts[1]!;
    const props = { onManageChannel: vi.fn() };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [observed] }} />);
    fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("inline-view-rollout-view-miners-action"));
    expect(screen.getByTestId("rollout-miners-modal")).toHaveTextContent("Canary, Proto Rig");
    await waitFor(() => expect(listRolloutDevices).toHaveBeenCalledOnce());
    const readSignal = listRolloutDevices.mock.calls[0]![1];

    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [{ ...observed, revision: 8n }, second] }} />);
    expect(screen.getByTestId("rollout-miners-modal")).toHaveTextContent("Canary, Proto Rig");
    expect(screen.getByTestId("rollout-miners-modal")).not.toHaveTextContent("Other Rig");
    expect(readSignal?.aborted).toBe(false);
    await act(async () => pending.resolve([]));
    expect(listRolloutDevices.mock.calls.every(([rolloutId]) => rolloutId === observed.id)).toBe(true);
    expect(screen.queryByTestId("inline-rollout-live-view")).not.toBeInTheDocument();

    fireEvent.click(within(screen.getByTestId("rollout-miners-modal")).getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByTestId("rollout-miners-modal")).not.toBeInTheDocument());
    expect(screen.getByTestId(`update-banner-${observed.id}`)).toBeInTheDocument();
    expect(screen.getByTestId(`update-banner-${second.id}`)).toBeInTheDocument();
    expect(screen.queryByTestId("inline-rollout-live-view")).not.toBeInTheDocument();
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
  });

  it("keeps an open miner list and its pending read when the only update completes", async () => {
    const observed = { ...activeRigRollout, revision: 7n };
    const completed = {
      ...observed,
      status: completedRigRollout.status,
      state: completedRigRollout.state,
      revision: 8n,
    };
    const pending = deferred<[]>();
    const api = apiFor(observed);
    api.listRolloutDevices.mockReturnValueOnce(pending.promise);
    const { rerender } = render(<ActiveUpdatesMonitor api={api} onManageChannel={vi.fn()} />);
    fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("inline-view-rollout-view-miners-action"));
    await waitFor(() => expect(api.listRolloutDevices).toHaveBeenCalledOnce());
    const readSignal = api.listRolloutDevices.mock.calls[0]![1];

    rerender(<ActiveUpdatesMonitor api={{ ...api, rollouts: [completed] }} onManageChannel={vi.fn()} />);
    expect(screen.queryByTestId("inline-rollout-live-view")).not.toBeInTheDocument();
    expect(screen.queryByTestId(/^update-banner-/)).not.toBeInTheDocument();
    expect(screen.getByTestId("rollout-miners-modal")).toHaveTextContent("Canary, Proto Rig");
    expect(readSignal?.aborted).toBe(false);
    await act(async () => pending.resolve([]));
    expect(api.listRolloutDevices.mock.calls.every(([rolloutId]) => rolloutId === observed.id)).toBe(true);

    expect(screen.getByTestId("rollout-miners-modal")).not.toHaveTextContent("evidence scope");
    fireEvent.click(within(screen.getByTestId("rollout-miners-modal")).getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByTestId("rollout-miners-modal")).not.toBeInTheDocument());
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
  });

  it.each(["success", "failure"])(
    "keeps retry locked across presentation changes and miner navigation (%s)",
    async (outcome) => {
      const observed = { ...activeRigRollout, revision: 7n };
      const api = apiWithBanners(observed);
      const pending = deferred<undefined>();
      api.retryFailedDevices.mockReturnValueOnce(pending.promise);
      const { rerender } = render(
        <ActiveUpdatesMonitor api={{ ...api, rollouts: [observed] }} onManageChannel={vi.fn()} />,
      );
      fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
      fireEvent.click(screen.getByTestId("inline-view-rollout-retry-action"));
      expect(screen.getByTestId("retry-rollout-dialog")).toHaveTextContent("including earlier updates");
      expect(screen.getByTestId("retry-rollout-dialog")).toHaveTextContent("does not advance review gates");
      expect(api.retryFailedDevices).not.toHaveBeenCalled();
      fireEvent.click(screen.getByTestId("confirm-rollout-retry"));
      expect(screen.getByTestId("inline-view-rollout-pause-action")).toBeDisabled();
      expect(screen.queryByTestId("retry-rollout-dialog")).not.toBeInTheDocument();

      rerender(<ActiveUpdatesMonitor api={api} onManageChannel={vi.fn()} />);
      fireEvent.click(within(screen.getByTestId(`update-banner-${observed.id}`)).getByRole("button"));
      expect(screen.getByRole("status")).toHaveTextContent("Requesting retry");
      fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
      expect(screen.getByTestId("view-rollout-retry-action")).toBeDisabled();
      expect(screen.getByTestId("view-rollout-cancel-action")).toBeDisabled();
      expect(screen.getByTestId("view-rollout-rollback-action")).toBeDisabled();
      fireEvent.click(screen.getByTestId("view-rollout-view-miners-action"));
      expect(screen.getByTestId("rollout-miners-modal")).toBeInTheDocument();
      await waitFor(() => expect(api.listRolloutDevices).toHaveBeenCalledOnce());
      expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(observed.id, 7n);
      expect(api.cancelRollout).not.toHaveBeenCalled();
      expect(api.rollbackFirmware).not.toHaveBeenCalled();
      await act(async () => {
        if (outcome === "success") pending.resolve(undefined);
        else pending.reject(new Error("The assignment changed"));
      });
      fireEvent.click(within(screen.getByTestId("rollout-miners-modal")).getByRole("button", { name: "Done" }));
      expect(screen.getByTestId("view-rollout-pause-action")).toBeEnabled();
    },
  );

  it("does not resurrect a retry confirmation when assignment eligibility returns", () => {
    const observed = { ...completedWithFailuresRigRollout, revision: 7n };
    const api = apiFor(observed);
    const props = { onManageChannel: vi.fn(), request: { kind: "view" as const, rollout: observed } };
    const { rerender } = render(<ActiveUpdatesMonitor {...props} api={api} />);
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    expect(screen.getByTestId("retry-rollout-dialog")).toBeInTheDocument();
    const successor = { ...activeRigRollout, id: observed.id + 1n };
    rerender(<ActiveUpdatesMonitor {...props} api={{ ...api, rollouts: [observed, successor] }} />);
    expect(screen.queryByTestId("retry-rollout-dialog")).not.toBeInTheDocument();
    rerender(<ActiveUpdatesMonitor {...props} api={api} />);
    expect(screen.queryByTestId("retry-rollout-dialog")).not.toBeInTheDocument();
    expect(api.retryFailedDevices).not.toHaveBeenCalled();
  });

  it("loads miner details only after the inline View miners action is selected", async () => {
    const api = apiFor(activeRigRollout);
    render(<ActiveUpdatesMonitor api={api} onManageChannel={vi.fn()} />);
    expect(api.listRolloutDevices).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("inline-view-rollout-view-miners-action"));

    expect(screen.getByTestId("rollout-miners-modal")).toBeInTheDocument();
    await waitFor(() =>
      expect(api.listRolloutDevices).toHaveBeenCalledExactlyOnceWith(activeRigRollout.id, expect.any(AbortSignal)),
    );
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
  });

  it("opens external history fullscreen and restores the inline update after dismissal", () => {
    const api = apiFor(activeRigRollout);
    const history = { ...completedWithFailuresRigRollout, id: 200n };
    function Harness() {
      const [request, setRequest] = useState<MonitorRequest | null>(null);
      return (
        <>
          <button onClick={() => setRequest({ kind: "view", rollout: history })}>View historical update</button>
          <ActiveUpdatesMonitor
            api={api}
            request={request}
            onRequestHandled={() => setRequest(null)}
            onManageChannel={vi.fn()}
          />
        </>
      );
    }
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "View historical update" }));
    expect(screen.getByTestId(`rollout-detail-${history.id}`)).toBeInTheDocument();
    expect(screen.getByTestId("rollout-live-view")).toBeInTheDocument();
    expect(screen.queryByTestId("inline-rollout-live-view")).not.toBeInTheDocument();
    expect(screen.queryByTestId("inline-view-rollout-pause-action")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByTestId("inline-rollout-live-view")).toBeInTheDocument();
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
    expect(api.listRolloutDevices).not.toHaveBeenCalled();
  });

  it.each([
    { state: "empty", rollouts: [] },
    { state: "completed", rollouts: [completedRigRollout] },
    { state: "canceled", rollouts: [canceledRemainingRigRollout] },
  ])("shows no active-update surface when the baseline is $state", ({ rollouts }) => {
    const api = apiFor(activeRigRollout);
    render(<ActiveUpdatesMonitor api={{ ...api, rollouts }} onManageChannel={vi.fn()} />);
    expect(screen.queryByTestId("inline-rollout-live-view")).not.toBeInTheDocument();
    expect(screen.queryByTestId("rollout-live-view")).not.toBeInTheDocument();
    expect(screen.queryByTestId(/^active-update-/)).not.toBeInTheDocument();
  });
});
