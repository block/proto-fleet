import { useEffect } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import Firmware from "./Firmware";
import { canaryChannel } from "./ReleaseChannels/ReleaseChannels.fixtures";
import { ReleaseChannelSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useFleetStore } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";

const { mockUseReleaseChannels, mockListFirmwareFiles } = vi.hoisted(() => ({
  mockUseReleaseChannels: vi.fn(),
  mockListFirmwareFiles: vi.fn(),
}));

vi.mock("@/protoFleet/api/useReleaseChannels", () => ({ useReleaseChannels: mockUseReleaseChannels }));
vi.mock("@/protoFleet/api/useFirmwareApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFirmwareApi")>()),
  useFirmwareApi: () => ({ listFirmwareFiles: mockListFirmwareFiles }),
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));
// Scope selection is unrelated to loading/recovery; keep the page, tab,
// management form, and their save/retry handlers real.
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: () => null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
}));

const apiFor = () => ({
  channels: [],
  rollouts: [],
  minerNames: {},
  isLoading: false,
  hasLoaded: true,
  error: null,
  refresh: vi.fn().mockResolvedValue(undefined),
  createChannel: vi.fn().mockResolvedValue(undefined),
  updateChannel: vi.fn().mockResolvedValue(undefined),
  deleteChannel: vi.fn().mockResolvedValue(undefined),
  previewScope: vi.fn().mockResolvedValue({ conflicts: [] }),
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

const page = (tab = "release-channels") => (
  <MemoryRouter initialEntries={[`/settings/firmware?tab=${tab}`]}>
    <Firmware />
  </MemoryRouter>
);

const initialAuth = useFleetStore.getState().auth;

beforeEach(() => {
  vi.clearAllMocks();
  mockListFirmwareFiles.mockResolvedValue([]);
  useFleetStore.setState({
    auth: { ...initialAuth, isAuthenticated: true, username: "operator", sessionGeneration: 1 },
  });
});

afterEach(() => useFleetStore.setState({ auth: initialAuth }));

describe("release-channel load errors", () => {
  it("shows the initial failure instead of an empty channel list, then recovers on retry", async () => {
    const api = { ...apiFor(), hasLoaded: false, error: new Error("The server is unavailable") };
    mockUseReleaseChannels.mockReturnValue(api);
    const { rerender } = render(page());

    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load release channels and update status");
    expect(screen.getByRole("alert")).toHaveTextContent("The server is unavailable");
    expect(screen.queryByText("No release channels")).not.toBeInTheDocument();
    expect(screen.queryByTestId("create-release-channel")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.refresh).toHaveBeenCalledOnce());

    mockUseReleaseChannels.mockReturnValue({ ...api, hasLoaded: true, error: null, channels: [canaryChannel] });
    rerender(page());
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByTestId("channel-row-Canary")).toBeInTheDocument();
    expect(pushToast).not.toHaveBeenCalled();
  });

  it("mounts channel polling only while the Release channels tab is open", async () => {
    const api = { ...apiFor(), hasLoaded: false, error: new Error("Request failed") };
    const mounted = vi.fn();
    const stopped = vi.fn();
    mockUseReleaseChannels.mockImplementation(function useMockReleaseChannels() {
      useEffect(() => {
        mounted();
        return stopped;
      }, []);
      return api;
    });
    const { unmount } = render(page("files"));

    await screen.findByText("No firmware files uploaded");
    expect(mockUseReleaseChannels).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole("button", { name: "Release channels" }));
    await waitFor(() => expect(mounted).toHaveBeenCalledOnce());
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load release channels and update status");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.refresh).toHaveBeenCalledOnce());
    fireEvent.mouseDown(screen.getByRole("button", { name: "Files" }));
    await waitFor(() => expect(stopped).toHaveBeenCalledOnce());
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole("button", { name: "Release channels" }));
    await waitFor(() => expect(mounted).toHaveBeenCalledTimes(2));
    unmount();
    expect(stopped).toHaveBeenCalledTimes(2);
  });

  it("handles a rejected manual retry without duplicate requests or failure toasts", async () => {
    const api = { ...apiFor(), hasLoaded: false, error: new Error("Request failed") };
    let rejectRetry!: (error: Error) => void;
    api.refresh.mockReturnValue(
      new Promise<void>((_, reject) => {
        rejectRetry = reject;
      }),
    );
    mockUseReleaseChannels.mockReturnValue(api);
    render(page());

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(screen.getByRole("alert")).toHaveAttribute("aria-busy", "true");
    fireEvent.click(screen.getByRole("button", { name: "Retrying..." }));
    expect(api.refresh).toHaveBeenCalledOnce();
    await act(async () => rejectRetry(new Error("Still unavailable")));

    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveAttribute("aria-busy", "false");
    expect(screen.queryByText("No release channels")).not.toBeInTheDocument();
    expect(pushToast).not.toHaveBeenCalled();
  });

  it("keeps loaded channels and an open draft visible when a later refresh fails", async () => {
    const api = { ...apiFor(), channels: [canaryChannel] };
    mockUseReleaseChannels.mockReturnValue(api);
    const { rerender } = render(page());
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Canary draft" } });

    mockUseReleaseChannels.mockReturnValue({ ...api, error: new Error("Request failed") });
    rerender(page());

    expect(screen.getByRole("alert")).toHaveTextContent("may be out of date");
    expect(screen.getByRole("alert")).toHaveTextContent("Showing the last loaded data");
    expect(screen.getByTestId("release-channel-Canary")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Canary draft");
    expect(pushToast).not.toHaveBeenCalled();
  });

  it("does not offer to resave an unchanged committed update while its refresh has failed", async () => {
    const api = { ...apiFor(), channels: [canaryChannel] };
    let finishUpdate!: () => void;
    api.updateChannel.mockReturnValue(
      new Promise<void>((resolve) => {
        finishUpdate = resolve;
      }),
    );
    mockUseReleaseChannels.mockReturnValue(api);
    const { rerender } = render(page());
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved name" } });
    fireEvent.click(screen.getByTestId("save-channel"));

    mockUseReleaseChannels.mockReturnValue({ ...api, error: new Error("Refresh failed") });
    rerender(page());
    await act(async () => finishUpdate());

    expect(screen.getByRole("alert")).toHaveTextContent("may be out of date");
    expect(screen.getByLabelText("Name")).toHaveValue("Saved name");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(api.updateChannel).toHaveBeenCalledOnce();
    expect(pushToast).toHaveBeenCalledWith({ message: "Release channel saved", status: "success" });
  });

  it.each([true, false])(
    "does not repeat a committed create after its refresh fails (channel still exists: %s)",
    async (exists) => {
      const api = apiFor();
      const created = create(ReleaseChannelSchema, { id: 12n, name: "New channel" });
      let finishCreate!: (channel: typeof created) => void;
      api.createChannel.mockReturnValue(
        new Promise<typeof created>((resolve) => {
          finishCreate = resolve;
        }),
      );
      mockUseReleaseChannels.mockReturnValue(api);
      const { rerender } = render(page());
      fireEvent.click(screen.getByTestId("create-release-channel"));
      fireEvent.change(screen.getByLabelText("Name"), { target: { value: created.name } });
      fireEvent.click(screen.getByTestId("save-channel"));

      mockUseReleaseChannels.mockReturnValue({ ...api, error: new Error("The save succeeded but refresh failed") });
      await act(async () => finishCreate(created));
      rerender(page());

      await waitFor(() =>
        expect(pushToast).toHaveBeenCalledWith({
          message: "Created release channel New channel",
          status: "success",
        }),
      );
      expect(screen.getByRole("alert")).toHaveTextContent("may be out of date");
      expect(screen.getByText("Channel details will appear after a successful refresh.")).toBeInTheDocument();
      expect(screen.queryByTestId("release-channel-new")).not.toBeInTheDocument();
      expect(screen.queryByTestId("create-release-channel")).not.toBeInTheDocument();
      expect(screen.queryByText("No release channels")).not.toBeInTheDocument();
      expect(api.createChannel).toHaveBeenCalledOnce();

      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      await waitFor(() => expect(api.refresh).toHaveBeenCalledOnce());
      mockUseReleaseChannels.mockReturnValue({
        ...api,
        channels: exists ? [{ ...created, modelGroups: [] }] : [],
      });
      rerender(page());

      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(screen.queryByText("Channel details will appear after a successful refresh.")).not.toBeInTheDocument();
      if (exists) {
        expect(screen.getByTestId("release-channel-New channel")).toBeInTheDocument();
      } else {
        expect(screen.getByText("No release channels")).toBeInTheDocument();
      }
      expect(api.createChannel).toHaveBeenCalledOnce();
    },
  );
});
