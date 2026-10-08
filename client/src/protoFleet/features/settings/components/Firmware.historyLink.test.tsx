import { MemoryRouter, useNavigate, useSearchParams } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import Firmware from "./Firmware";
import { deferred, releaseChannelsApi } from "./ReleaseChannels/__tests__/helpers";
import { apiWithBanners } from "./ReleaseChannels/__tests__/monitorHelpers";
import {
  activeRigRollout,
  canaryChannel,
  completedRigRollout,
  completedWithFailuresRigRollout,
} from "./ReleaseChannels/ReleaseChannels.fixtures";
import { LINKED_ROLLOUT_PARAM, linkedRolloutPath } from "./ReleaseChannels/useLinkedRollout";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { RELEASE_CHANNELS_PATH } from "@/protoFleet/components/PageHeader/RolloutPill";
import { useFleetStore } from "@/protoFleet/store";

const { getRollout, useChannels, listFirmwareFiles } = vi.hoisted(() => ({
  getRollout: vi.fn(),
  useChannels: vi.fn(),
  listFirmwareFiles: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({ rolloutClient: { getRollout } }));
vi.mock("@/protoFleet/api/useReleaseChannels", () => ({ useReleaseChannels: useChannels }));
vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({ listFirmwareFiles }),
}));
vi.mock("./ReleaseChannels/ReleaseChannelsTab", () => ({
  default: ({
    onViewRollout,
    onRollbackRollout,
  }: {
    onViewRollout: (rollout: Rollout) => void;
    onRollbackRollout: (rollout: Rollout) => void;
  }) => (
    <>
      <p>Release channels table</p>
      <button onClick={() => onViewRollout(activeRigRollout)}>View channel update</button>
      <button onClick={() => onRollbackRollout(activeRigRollout)}>Roll back channel update</button>
    </>
  ),
}));

const initialAuth = useFleetStore.getState().auth;
const historical = { ...completedRigRollout, channelId: canaryChannel.id, channelName: canaryChannel.name };
function Navigation() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  return (
    <>
      <output data-testid="query">{params.toString()}</output>
      <button onClick={() => navigate(linkedRolloutPath(123n))}>Another update</button>
    </>
  );
}
function page(id = historical.id.toString()) {
  return (
    <MemoryRouter initialEntries={[`${RELEASE_CHANNELS_PATH}&${LINKED_ROLLOUT_PARAM}=${id}`]}>
      <Navigation />
      <Firmware />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useFleetStore.setState({
    auth: {
      ...initialAuth,
      username: "operator",
      isAuthenticated: true,
      sessionGeneration: 1,
      permissions: ["miner:firmware_update"],
    },
  });
  useChannels.mockReturnValue({ ...releaseChannelsApi(), channels: [canaryChannel], rollouts: [] });
  getRollout.mockResolvedValue({ rollout: historical });
  listFirmwareFiles.mockResolvedValue([]);
});
afterEach(() => useFleetStore.setState({ auth: initialAuth }));

describe("firmware history links", () => {
  it("opens a finished update outside the active cache and removes the link on dismissal", async () => {
    render(page());
    expect(await screen.findByTestId(`rollout-detail-${historical.id}`)).toBeInTheDocument();
    expect(getRollout).toHaveBeenCalledWith(
      { rolloutId: historical.id },
      expect.objectContaining({ timeoutMs: 30_000 }),
    );
    fireEvent.click(screen.getByRole("button", { name: /^Back$/ }));
    await waitFor(() => expect(screen.queryByTestId(`rollout-detail-${historical.id}`)).not.toBeInTheDocument());
    expect(screen.getByTestId("query")).toHaveTextContent("tab=release-channels");
    expect(screen.getByTestId("query")).not.toHaveTextContent("rollout=");
  });

  it("waits for channel data before opening the requested update", async () => {
    const ready = useChannels();
    useChannels.mockReturnValue({ ...ready, channels: [], hasLoaded: false, isLoading: true });
    const { rerender } = render(page());
    await waitFor(() => expect(getRollout).toHaveBeenCalledOnce());
    expect(screen.getByText("Loading firmware update...")).toHaveAttribute("role", "status");
    expect(screen.queryByTestId(`rollout-detail-${historical.id}`)).not.toBeInTheDocument();
    useChannels.mockReturnValue(ready);
    rerender(page());
    expect(await screen.findByTestId(`rollout-detail-${historical.id}`)).toBeInTheDocument();
  });

  it("can retry a failed history read", async () => {
    getRollout.mockRejectedValueOnce(new Error("This update is unavailable"));
    render(page());
    expect(await screen.findByRole("alert")).toHaveTextContent("This update is unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByTestId(`rollout-detail-${historical.id}`)).toBeInTheDocument();
  });

  it("opens a retry successor even when channel polling rerenders the history link", async () => {
    const observed = completedWithFailuresRigRollout;
    const successor = { ...activeRigRollout, id: 200n, assignmentGeneration: observed.assignmentGeneration };
    const pending = deferred<typeof successor>();
    const api = releaseChannelsApi();
    api.channels = [
      {
        ...canaryChannel,
        id: observed.channelId,
        modelGroups: canaryChannel.modelGroups.map((group) => ({
          ...group,
          activeRolloutId: 0n,
          assignmentGeneration: observed.assignmentGeneration,
          firmwareChecksum: observed.firmwareChecksum,
        })),
      },
    ];
    api.retryFailedDevices.mockReturnValue(pending.promise);
    useChannels.mockReturnValue(api);
    getRollout.mockResolvedValue({ rollout: observed });
    const { rerender } = render(page(observed.id.toString()));
    await screen.findByTestId(`rollout-detail-${observed.id}`);
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    fireEvent.click(screen.getByTestId("confirm-rollout-retry"));
    expect(api.retryFailedDevices).toHaveBeenCalledExactlyOnceWith(observed.id, observed.revision);

    useChannels.mockReturnValue({ ...api, channels: [...api.channels] });
    rerender(page(observed.id.toString()));
    await act(async () => pending.resolve(successor));
    expect(await screen.findByTestId(`rollout-detail-${successor.id}`)).toBeInTheDocument();
    expect(screen.getByTestId("query")).not.toHaveTextContent("rollout=");
  });

  it.each(["abc", "0", "-1", "9223372036854775808"])("rejects invalid link %s without a request", async (id) => {
    const api = useChannels();
    render(page(id));
    expect(screen.getByRole("alert")).toHaveTextContent("This firmware update link is invalid");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(getRollout).not.toHaveBeenCalled();
    expect(api.refresh).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByTestId("query")).toHaveTextContent("tab=release-channels");
    expect(screen.getByTestId("query")).not.toHaveTextContent("rollout=");
  });

  it("does not open a deleted channel's update and retries the channel scan once at a time", async () => {
    const scan = deferred();
    const api = { ...releaseChannelsApi(), channels: [], rollouts: [] };
    api.refresh.mockReturnValue(scan.promise);
    useChannels.mockReturnValue(api);
    render(page());
    expect(await screen.findByRole("alert")).toHaveTextContent("channel is no longer available");
    expect(screen.queryByTestId(`rollout-detail-${historical.id}`)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    fireEvent.click(screen.getByRole("button", { name: "Retrying..." }));
    expect(api.refresh).toHaveBeenCalledOnce();
    expect(getRollout).toHaveBeenCalledOnce();
    expect(screen.getByRole("alert")).toHaveAttribute("aria-busy", "true");
    await act(async () => scan.resolve());
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveAttribute("aria-busy", "false");
  });

  it.each([
    ["View channel update", true],
    ["Roll back channel update", false],
  ])("lets %s from the channels tab replace a pending link", async (action, opensDetail) => {
    const pending = deferred<{ rollout: typeof historical }>();
    getRollout.mockReturnValueOnce(pending.promise);
    render(page());
    await waitFor(() => expect(getRollout).toHaveBeenCalledOnce());
    fireEvent.click(screen.getByRole("button", { name: action }));
    expect(screen.getByTestId("query")).not.toHaveTextContent("rollout=");
    await act(async () => pending.resolve({ rollout: historical }));
    expect(screen.queryByTestId(`rollout-detail-${historical.id}`)).not.toBeInTheDocument();
    if (opensDetail) expect(screen.getByTestId(`rollout-detail-${activeRigRollout.id}`)).toBeInTheDocument();
  });

  it("keeps an update opened from a banner when a pending link resolves later", async () => {
    const pending = deferred<{ rollout: typeof historical }>();
    getRollout.mockReturnValueOnce(pending.promise);
    useChannels.mockReturnValue(apiWithBanners(activeRigRollout));
    render(page());
    await waitFor(() => expect(getRollout).toHaveBeenCalledOnce());
    expect(screen.queryByTestId(`rollout-detail-${historical.id}`)).not.toBeInTheDocument();
    fireEvent.click(
      within(screen.getByTestId(`update-banner-${activeRigRollout.id}`)).getByRole("button", { name: "View update" }),
    );
    expect(screen.getByTestId(`rollout-detail-${activeRigRollout.id}`)).toBeInTheDocument();
    expect(screen.getByTestId("query")).not.toHaveTextContent("rollout=");
    await act(async () => pending.resolve({ rollout: historical }));
    expect(screen.getByTestId(`rollout-detail-${activeRigRollout.id}`)).toBeInTheDocument();
    expect(screen.queryByTestId(`rollout-detail-${historical.id}`)).not.toBeInTheDocument();
  });

  it("ignores late responses when navigation selects a different update", async () => {
    const old = deferred<{ rollout: typeof historical }>();
    getRollout.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ rollout: { ...historical, id: 123n } });
    render(page());
    fireEvent.click(screen.getByRole("button", { name: "Another update" }));
    expect(await screen.findByTestId("rollout-detail-123")).toBeInTheDocument();
    await act(async () => old.resolve({ rollout: historical }));
    expect(screen.queryByTestId(`rollout-detail-${historical.id}`)).not.toBeInTheDocument();
    expect(screen.getByTestId("rollout-detail-123")).toBeInTheDocument();
  });

  it("does not fetch without firmware permission and hides results when the session changes", async () => {
    useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, permissions: [] } });
    render(page());
    expect(getRollout).not.toHaveBeenCalled();
    act(() =>
      useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, permissions: ["miner:firmware_update"] } }),
    );
    expect(await screen.findByTestId(`rollout-detail-${historical.id}`)).toBeInTheDocument();
    act(() =>
      useFleetStore.setState({
        auth: { ...useFleetStore.getState().auth, isAuthenticated: false, sessionGeneration: 2 },
      }),
    );
    expect(screen.queryByTestId(`rollout-detail-${historical.id}`)).not.toBeInTheDocument();
  });
});
