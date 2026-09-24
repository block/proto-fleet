import { useEffect } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import Firmware from "./Firmware";
import {
  releaseChannelsApi as apiFor,
  closeChannelSettings,
  deferred,
  openChannelSettings,
} from "./ReleaseChannels/__tests__/helpers";
import {
  canaryChannel,
  channelWithActiveRollout,
  firmwareFiles,
  gatedRigRollout,
  productionChannel,
} from "./ReleaseChannels/ReleaseChannels.fixtures";
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
// Keep the page, tab, management form, and save/retry handlers real; the
// shared scope pickers' fleet loading is outside these navigation regressions.
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: ({ open, onSave }: { open: boolean; onSave: (selection: { siteIds: string[] }) => void }) =>
    open ? <button onClick={() => onSave({ siteIds: ["2"] })}>Choose site 2</button> : null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
}));

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

describe("update detail Manage navigation", () => {
  const productionRollout = {
    ...gatedRigRollout,
    id: 102n,
    channelId: productionChannel.id,
    channelName: productionChannel.name,
  };

  const navigationApi = () => {
    const api = {
      ...apiFor(),
      channels: [
        channelWithActiveRollout(canaryChannel, gatedRigRollout),
        channelWithActiveRollout(productionChannel, productionRollout),
      ],
      rollouts: [gatedRigRollout, productionRollout],
    };
    api.listChannelRollouts.mockImplementation(async (channelId) =>
      api.rollouts.filter((rollout) => rollout.channelId === channelId),
    );
    mockUseReleaseChannels.mockReturnValue(api);
    return api;
  };

  const manageFromBanner = (rollout = productionRollout) => {
    closeChannelSettings();
    fireEvent.click(
      within(screen.getByTestId(`active-update-${rollout.id}`)).getByRole("button", { name: "Review update" }),
    );
    fireEvent.click(screen.getByTestId("view-rollout-manage-action"));
  };

  const stageFirmwareClear = async () => {
    closeChannelSettings();
    await waitFor(() => expect(screen.queryByText("Loading firmware files...")).not.toBeInTheDocument());
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    expect(screen.getByText("1 firmware change pending")).toBeInTheDocument();
  };

  beforeEach(() => {
    mockListFirmwareFiles.mockResolvedValue(firmwareFiles);
    useFleetStore.setState((state) => ({
      auth: { ...state.auth, permissions: ["site:read", "miner:firmware_update"] },
    }));
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });

  afterEach(() => vi.restoreAllMocks());

  it.each(["banner", "history"])("preserves drafts when returning to the same channel from %s", async (entry) => {
    const api = navigationApi();
    render(page());
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Canary draft" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Keep this description" } });
    fireEvent.change(screen.getByLabelText("Pilot batch size (miners)"), { target: { value: "3" } });
    fireEvent.click(screen.getByRole("button", { name: /^Sites/ }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 2" }));
    closeChannelSettings();

    if (entry === "history") {
      fireEvent.click(screen.getByTestId("channel-history"));
      fireEvent.click(await screen.findByTestId(`history-view-${gatedRigRollout.id}`));
      fireEvent.click(screen.getByTestId("view-rollout-manage-action"));
    } else {
      await stageFirmwareClear();
      manageFromBanner(gatedRigRollout);
    }

    expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument();
    expect(screen.queryByTestId("rollout-detail-header")).not.toBeInTheDocument();
    if (entry === "banner") {
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
      expect(screen.getByText("1 firmware change pending")).toBeInTheDocument();
      expect(screen.queryByTestId("channel-settings")).not.toBeInTheDocument();
      expect(screen.queryByTestId("channel-history")).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    }
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Canary draft");
    expect(screen.getByLabelText("Description")).toHaveValue("Keep this description");
    expect(screen.getByLabelText("Pilot batch size (miners)")).toHaveValue("3");
    expect(api.updateChannel).not.toHaveBeenCalled();
    expect(api.applyFirmware).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("save-channel"));
    await waitFor(() => expect(api.updateChannel).toHaveBeenCalledOnce());
    expect(api.updateChannel.mock.calls[0]).toEqual([
      canaryChannel.id,
      expect.objectContaining({
        name: "Canary draft",
        description: "Keep this description",
        scope: expect.objectContaining({ siteIds: [2n] }),
        behavior: expect.objectContaining({ pilotSize: 3 }),
      }),
    ]);
    expect(screen.queryByText("1 firmware change pending")).not.toBeInTheDocument();
  });

  it.each(["settings", "firmware"])(
    "asks before discarding unsaved %s to manage a different channel",
    async (draft) => {
      const api = navigationApi();
      render(page());
      fireEvent.click(screen.getByTestId("manage-channel-Canary"));
      if (draft === "settings") {
        openChannelSettings();
        fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Unsaved name" } });
      } else {
        await stageFirmwareClear();
      }

      manageFromBanner();
      const confirmation = within(screen.getByTestId("discard-channel-changes-dialog"));
      expect(confirmation.getByText("Discard unsaved channel changes?")).toBeInTheDocument();
      expect(screen.getByTestId("release-channel-Canary")).toBeInTheDocument();
      fireEvent.click(confirmation.getByRole("button", { name: "Keep editing" }));
      await waitFor(() => expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument());
      if (draft === "settings") {
        openChannelSettings();
        expect(screen.getByLabelText("Name")).toHaveValue("Unsaved name");
      } else {
        expect(screen.getByText("1 firmware change pending")).toBeInTheDocument();
        expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("No firmware");
      }

      manageFromBanner();
      fireEvent.click(
        within(screen.getByTestId("discard-channel-changes-dialog")).getByRole("button", { name: "Discard changes" }),
      );
      expect(screen.getByTestId("release-channel-Production")).toBeInTheDocument();
      openChannelSettings();
      expect(screen.getByLabelText("Name")).toHaveValue("Production");
      expect(screen.queryByText("1 firmware change pending")).not.toBeInTheDocument();
      await waitFor(() => expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument());
      expect(api.updateChannel).not.toHaveBeenCalled();
      expect(api.applyFirmware).not.toHaveBeenCalled();
      manageFromBanner(gatedRigRollout);
      openChannelSettings();
      expect(screen.getByLabelText("Name")).toHaveValue("Canary");
      expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent("1.4.4");
      expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument();
    },
  );

  it.each(["list", "pristine editor", "files"])("opens the requested channel directly from %s", async (origin) => {
    navigationApi();
    render(page(origin === "files" ? "files" : "release-channels"));
    if (origin === "pristine editor") fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    manageFromBanner();
    expect(await screen.findByTestId("release-channel-Production")).toBeInTheDocument();
    expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument();
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Production");
  });

  it("keeps a new-channel draft until the operator confirms navigation", async () => {
    const api = navigationApi();
    render(page());
    fireEvent.click(screen.getByTestId("create-release-channel"));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New channel draft" } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Unfinished setup" } });
    manageFromBanner();
    fireEvent.click(
      within(screen.getByTestId("discard-channel-changes-dialog")).getByRole("button", { name: "Keep editing" }),
    );
    expect(screen.getByLabelText("Name")).toHaveValue("New channel draft");
    expect(screen.getByLabelText("Description")).toHaveValue("Unfinished setup");
    manageFromBanner();
    fireEvent.click(
      within(screen.getByTestId("discard-channel-changes-dialog")).getByRole("button", { name: "Discard changes" }),
    );
    expect(screen.getByTestId("release-channel-Production")).toBeInTheDocument();
    expect(api.createChannel).not.toHaveBeenCalled();
    expect(api.updateChannel).not.toHaveBeenCalled();
  });

  it.each(["success", "failure"])("waits for a settings write before handling navigation after %s", async (result) => {
    const api = navigationApi();
    const saved = deferred<undefined>();
    api.updateChannel.mockReturnValueOnce(saved.promise);
    render(page());
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved name" } });
    fireEvent.click(screen.getByTestId("save-channel"));
    manageFromBanner();

    expect(screen.getByTestId("release-channel-Canary")).toBeInTheDocument();
    expect(screen.queryByTestId("release-channel-Production")).not.toBeInTheDocument();
    expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument();
    expect(api.updateChannel).toHaveBeenCalledOnce();
    if (result === "success") {
      await act(async () => saved.resolve(undefined));
      expect(await screen.findByTestId("release-channel-Production")).toBeInTheDocument();
      expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument();
    } else {
      await act(async () => saved.reject(new Error("Save failed")));
      fireEvent.click(
        within(await screen.findByTestId("discard-channel-changes-dialog")).getByRole("button", {
          name: "Keep editing",
        }),
      );
      openChannelSettings();
      expect(screen.getByLabelText("Name")).toHaveValue("Saved name");
      expect(screen.getByTestId("save-channel")).toBeEnabled();
    }
    expect(api.updateChannel).toHaveBeenCalledOnce();
  });

  it("waits for firmware application before navigating without a discard prompt", async () => {
    const api = navigationApi();
    const applied = deferred<[]>();
    api.applyFirmware.mockReturnValueOnce(applied.promise);
    render(page());
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    await stageFirmwareClear();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    fireEvent.click(
      within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Clear assignments" }),
    );
    manageFromBanner();
    expect(screen.getByTestId("release-channel-Canary")).toBeInTheDocument();
    expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument();
    expect(api.applyFirmware).toHaveBeenCalledOnce();
    await act(async () => applied.resolve([]));
    expect(await screen.findByTestId("release-channel-Production")).toBeInTheDocument();
    expect(screen.queryByTestId("discard-channel-changes-dialog")).not.toBeInTheDocument();
    expect(api.applyFirmware).toHaveBeenCalledOnce();
  });
});

describe("release-channel load errors", () => {
  it.each(["files", "release-channels"])(
    "shows polling failures and retry recovery beside the open update controls on the %s tab",
    async (tab) => {
      const api = { ...apiFor(), channels: [canaryChannel], rollouts: [gatedRigRollout] };
      mockUseReleaseChannels.mockReturnValue(api);
      const { rerender } = render(page(tab));
      fireEvent.click(screen.getByRole("button", { name: "Review update" }));
      const header = within(screen.getByTestId("rollout-detail-header"));
      expect(header.getByRole("button", { name: "Continue" })).toBeInTheDocument();
      expect(header.queryByRole("alert")).not.toBeInTheDocument();

      const staleApi = { ...api, error: new Error("Polling failed") };
      mockUseReleaseChannels.mockReturnValue(staleApi);
      rerender(page(tab));
      expect(header.getByRole("alert")).toHaveTextContent("update status may be out of date");
      expect(header.getByRole("alert")).toHaveTextContent("Showing the last loaded data");
      expect(screen.getByTestId("rollout-performance")).toBeInTheDocument();

      const retry = deferred();
      api.refresh.mockReturnValueOnce(retry.promise);
      fireEvent.click(header.getByRole("button", { name: "Retry" }));
      expect(header.getByRole("alert")).toHaveAttribute("aria-busy", "true");
      fireEvent.click(header.getByRole("button", { name: "Retrying..." }));
      expect(api.refresh).toHaveBeenCalledOnce();
      await act(async () => retry.reject(new Error("Still unavailable")));
      expect(header.getByRole("alert")).toHaveAttribute("aria-busy", "false");
      expect(header.getByRole("button", { name: "Retry" })).toBeInTheDocument();

      fireEvent.click(header.getByRole("button", { name: "Retry" }));
      await waitFor(() => expect(api.refresh).toHaveBeenCalledTimes(2));
      mockUseReleaseChannels.mockReturnValue(api);
      rerender(page(tab));
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(header.getByRole("button", { name: "Continue" })).toBeInTheDocument();
      expect(api.continueRollout).not.toHaveBeenCalled();
      expect(pushToast).not.toHaveBeenCalled();
    },
  );

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

  it("keeps one polling owner for the active-update monitor across both tabs", async () => {
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
    expect(mounted).toHaveBeenCalledOnce();
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load release channels and update status");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.refresh).toHaveBeenCalledOnce());
    fireEvent.mouseDown(screen.getByRole("button", { name: "Release channels" }));
    await waitFor(() => expect(screen.queryByText("No firmware files uploaded")).not.toBeInTheDocument());
    expect(mounted).toHaveBeenCalledOnce();
    expect(stopped).not.toHaveBeenCalled();
    fireEvent.mouseDown(screen.getByRole("button", { name: "Files" }));
    await screen.findByText("No firmware files uploaded");
    expect(mounted).toHaveBeenCalledOnce();
    expect(stopped).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load release channels and update status");
    unmount();
    expect(stopped).toHaveBeenCalledOnce();
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
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Canary draft" } });

    mockUseReleaseChannels.mockReturnValue({ ...api, error: new Error("Request failed") });
    rerender(page());

    expect(screen.getByRole("alert")).toHaveTextContent("may be out of date");
    expect(screen.getByRole("alert")).toHaveTextContent("Showing the last loaded data");
    expect(screen.getByTestId("release-channel-Canary")).toBeInTheDocument();
    openChannelSettings();
    expect(screen.getByLabelText("Name")).toHaveValue("Canary draft");
    expect(pushToast).not.toHaveBeenCalled();
  });

  it("does not offer to resave an unchanged committed update while its refresh has failed", async () => {
    const api = { ...apiFor(), channels: [canaryChannel] };
    const update = deferred<undefined>();
    api.updateChannel.mockReturnValue(update.promise);
    mockUseReleaseChannels.mockReturnValue(api);
    const { rerender } = render(page());
    fireEvent.click(screen.getByTestId("manage-channel-Canary"));
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Saved name" } });
    fireEvent.click(screen.getByTestId("save-channel"));

    mockUseReleaseChannels.mockReturnValue({ ...api, error: new Error("Refresh failed") });
    rerender(page());
    await act(async () => update.resolve(undefined));

    expect(screen.getByRole("alert")).toHaveTextContent("may be out of date");
    openChannelSettings();
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
