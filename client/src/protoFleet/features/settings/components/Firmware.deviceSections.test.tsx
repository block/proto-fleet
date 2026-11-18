import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Firmware from "./Firmware";
import { useFleetStore } from "@/protoFleet/store";

const mocks = vi.hoisted(() => ({
  listFirmwareFiles: vi.fn(),
  listPackages: vi.fn(),
  hasUpdatableContainer: vi.fn(),
  useContainers: vi.fn(),
}));

vi.mock("@/protoFleet/api/useContainers", () => ({ useContainers: mocks.useContainers }));
vi.mock("@/protoFleet/api/useFirmwareApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFirmwareApi")>()),
  useFirmwareApi: () => ({ listFirmwareFiles: mocks.listFirmwareFiles }),
}));
vi.mock("@/protoFleet/api/useFrigatePackages", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFrigatePackages")>()),
  useFrigatePackages: () => ({
    listPackages: mocks.listPackages,
    hasUpdatableContainer: mocks.hasUpdatableContainer,
    deletePackage: vi.fn(),
    uploadPackage: vi.fn(),
  }),
}));
vi.mock("@/protoFleet/api/useReleaseChannels", () => ({
  useReleaseChannels: () => ({
    channels: [],
    rollouts: [],
    acknowledgedRollbacks: [],
    minerNames: {},
    isLoading: false,
    hasLoaded: true,
    error: null,
    refresh: vi.fn(),
  }),
}));

const initialAuth = useFleetStore.getState().auth;
const setPermissions = (permissions: string[]) =>
  useFleetStore.setState({ auth: { ...initialAuth, isAuthenticated: true, username: "operator", permissions } });

const page = (tab?: string) => (
  <MemoryRouter initialEntries={[tab ? `/settings/firmware?tab=${tab}` : "/settings/firmware"]}>
    <Firmware />
  </MemoryRouter>
);

const rigFile = {
  id: "rig",
  filename: "rig-1.4.3.swu",
  size: 1024,
  uploaded_at: "2026-06-01T12:00:00Z",
  target_manufacturer: "Proto",
  target_model: "Rig",
};
const hashboardFile = {
  id: "hashboard",
  filename: "hashboard-firmware-1.39.0-prod.zip",
  size: 2048,
  uploaded_at: "2026-06-02T12:00:00Z",
  target_manufacturer: "Proto",
  target_model: "CT1",
  hashboard_build: "prod",
};

const section = (heading: string) => {
  const found = screen.getByRole("heading", { name: heading }).closest("section");
  if (!found) throw new Error(`No section for heading ${heading}`);
  return within(found);
};

// The permissions ListContainers needs before the page can tell whether a container is paired.
const containerRead = ["site:read", "fleet:read", "miner:read"];
const containerCatalog = (containers: unknown[], state: { error?: string; loading?: boolean } = {}) => ({
  containers,
  error: state.error,
  loading: state.loading ?? false,
  refetch: vi.fn(),
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listFirmwareFiles.mockResolvedValue([]);
  mocks.listPackages.mockResolvedValue([]);
  mocks.hasUpdatableContainer.mockResolvedValue(false);
  mocks.useContainers.mockReturnValue(containerCatalog([{ deviceIdentifier: "ct1-sim-001" }]));
});
afterEach(() => useFleetStore.setState({ auth: initialAuth }));

describe("Firmware files by device type", () => {
  it("lists miner firmware without a device heading when nothing belongs to a container", async () => {
    setPermissions(["miner:firmware_update"]);
    mocks.listFirmwareFiles.mockResolvedValue([rigFile]);
    render(page());

    expect(await screen.findByText(rigFile.filename)).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Miners" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Proto Containers" })).not.toBeInTheDocument();
  });

  it("separates container hashboard firmware from miner firmware", async () => {
    setPermissions(["miner:firmware_update"]);
    mocks.listFirmwareFiles.mockResolvedValue([rigFile, hashboardFile]);
    render(page());

    await screen.findByText(rigFile.filename);
    expect(section("Miners").getByText(rigFile.filename)).toBeInTheDocument();
    expect(section("Miners").queryByText(hashboardFile.filename)).not.toBeInTheDocument();
    expect(section("Hashboard firmware").getByText(hashboardFile.filename)).toBeInTheDocument();
    expect(section("Proto Containers").getByRole("heading", { name: "Hashboard firmware" })).toBeInTheDocument();
    expect(section("Proto Containers").queryByText(rigFile.filename)).not.toBeInTheDocument();
  });

  it("shows only the container section when every file targets a container", async () => {
    setPermissions(["miner:firmware_update"]);
    mocks.listFirmwareFiles.mockResolvedValue([hashboardFile]);
    render(page());

    expect(await screen.findByText(hashboardFile.filename)).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Miners" })).not.toBeInTheDocument();
    expect(screen.queryByText("No firmware files uploaded")).not.toBeInTheDocument();
  });
});

describe("Controller software on the firmware page", () => {
  it("are not listed or loaded without the software update permission", async () => {
    setPermissions(["miner:firmware_update"]);
    render(page());

    await screen.findByText("No firmware files uploaded");
    expect(screen.queryByRole("heading", { name: "Controller software" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Proto Containers" })).not.toBeInTheDocument();
    expect(mocks.listPackages).not.toHaveBeenCalled();
  });

  it("are listed under Proto Containers on the Files tab, which has no tab of their own", async () => {
    setPermissions(["miner:firmware_update", "controller:software_update"]);
    mocks.listFirmwareFiles.mockResolvedValue([rigFile, hashboardFile]);
    render(page());

    await screen.findByText(rigFile.filename);
    const containers = section("Proto Containers");
    expect(await containers.findByText("No controller software uploaded")).toBeInTheDocument();
    expect(containers.getByRole("heading", { name: "Controller software" })).toBeInTheDocument();
    expect(containers.getByRole("button", { name: "Upload controller software" })).toBeInTheDocument();
    expect(containers.getByText(hashboardFile.filename)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Upload firmware" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Files" })).toHaveAttribute("aria-current", "page");
    expect(screen.queryByRole("button", { name: "Controller software" })).not.toBeInTheDocument();
  });

  it("stay available next to the empty firmware state", async () => {
    setPermissions(["miner:firmware_update", "controller:software_update"]);
    render(page());

    expect(await screen.findByText("No firmware files uploaded")).toBeInTheDocument();
    expect(await section("Proto Containers").findByText("No controller software uploaded")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Miners" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Hashboard firmware" })).not.toBeInTheDocument();
  });

  it("open on the Files tab from a link to the retired tab", async () => {
    setPermissions(["miner:firmware_update", "controller:software_update"]);
    render(page("frigate-packages"));

    expect(await section("Proto Containers").findByText("No controller software uploaded")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Files" })).toHaveAttribute("aria-current", "page");
  });

  it("are the whole page for a user who cannot manage miner firmware", async () => {
    setPermissions(["controller:software_update"]);
    render(page());

    expect(await section("Proto Containers").findByText("No controller software uploaded")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Upload controller software" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Files" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Release channels" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Upload firmware" })).not.toBeInTheDocument();
    expect(mocks.listFirmwareFiles).not.toHaveBeenCalled();
  });
});

describe("Firmware files when no Proto Container is paired", () => {
  const everything = ["miner:firmware_update", "controller:software_update", ...containerRead];

  it("hide the Proto Containers section and list every file together", async () => {
    setPermissions(everything);
    mocks.useContainers.mockReturnValue(containerCatalog([]));
    mocks.listFirmwareFiles.mockResolvedValue([rigFile, hashboardFile]);
    render(page());

    expect(await screen.findByText(rigFile.filename)).toBeInTheDocument();
    expect(screen.getByText(hashboardFile.filename)).toBeInTheDocument();
    for (const heading of ["Miners", "Proto Containers", "Hashboard firmware", "Controller software"]) {
      expect(screen.queryByRole("heading", { name: heading })).not.toBeInTheDocument();
    }
    expect(screen.queryByRole("button", { name: "Upload controller software" })).not.toBeInTheDocument();
    expect(mocks.listPackages).not.toHaveBeenCalled();
    // The empty list alone did not decide it for a user who may update controller software.
    expect(mocks.hasUpdatableContainer).toHaveBeenCalledTimes(1);
  });

  it("hide the section without asking the server when the user cannot update controller software", async () => {
    setPermissions(["miner:firmware_update", ...containerRead]);
    mocks.useContainers.mockReturnValue(containerCatalog([]));
    mocks.listFirmwareFiles.mockResolvedValue([rigFile, hashboardFile]);
    render(page());

    expect(await screen.findByText(rigFile.filename)).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Proto Containers" })).not.toBeInTheDocument();
    expect(mocks.hasUpdatableContainer).not.toHaveBeenCalled();
  });

  // Reading containers and updating controller software are separate
  // permissions, so a site override can leave the list empty for someone who
  // may still update a container there.
  it("keep controller software for a user who may update a container they cannot list", async () => {
    setPermissions(everything);
    mocks.useContainers.mockReturnValue(containerCatalog([]));
    mocks.hasUpdatableContainer.mockResolvedValue(true);
    mocks.listFirmwareFiles.mockResolvedValue([rigFile]);
    render(page());

    await screen.findByText(rigFile.filename);
    expect(await section("Proto Containers").findByText("No controller software uploaded")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Upload controller software" })).toBeInTheDocument();
  });

  it("wait for the server's answer before hiding the section", async () => {
    setPermissions(everything);
    mocks.useContainers.mockReturnValue(containerCatalog([]));
    mocks.hasUpdatableContainer.mockReturnValue(new Promise(() => {}));
    mocks.listFirmwareFiles.mockResolvedValue([rigFile]);
    render(page());

    await waitFor(() => expect(mocks.hasUpdatableContainer).toHaveBeenCalled());
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByText("Loading firmware files...")).toBeInTheDocument();
    expect(screen.queryByText(rigFile.filename)).not.toBeInTheDocument();
  });

  it("keep the section when the server cannot say", async () => {
    setPermissions(everything);
    mocks.useContainers.mockReturnValue(containerCatalog([]));
    mocks.hasUpdatableContainer.mockRejectedValue(new Error("unavailable"));
    mocks.listFirmwareFiles.mockResolvedValue([rigFile]);
    render(page());

    await screen.findByText(rigFile.filename);
    expect(await section("Proto Containers").findByText("No controller software uploaded")).toBeInTheDocument();
  });

  it("show the section once a container is paired", async () => {
    setPermissions(everything);
    mocks.listFirmwareFiles.mockResolvedValue([rigFile, hashboardFile]);
    render(page());

    await screen.findByText(rigFile.filename);
    expect(section("Proto Containers").getByText(hashboardFile.filename)).toBeInTheDocument();
    expect(await section("Proto Containers").findByText("No controller software uploaded")).toBeInTheDocument();
    expect(mocks.useContainers).toHaveBeenCalledWith([], false, true);
  });

  it("wait for the container check before choosing a layout", async () => {
    setPermissions(everything);
    mocks.useContainers.mockReturnValue(containerCatalog([], { loading: true }));
    mocks.listFirmwareFiles.mockResolvedValue([rigFile, hashboardFile]);
    render(page());

    await waitFor(() => expect(mocks.listFirmwareFiles).toHaveBeenCalled());
    // Let the file list settle: the page must still wait for the container check.
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByText("Loading firmware files...")).toBeInTheDocument();
    expect(screen.queryByText(rigFile.filename)).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Proto Containers" })).not.toBeInTheDocument();
    expect(mocks.listPackages).not.toHaveBeenCalled();
  });

  it("keep the section when the container check fails", async () => {
    setPermissions(everything);
    mocks.useContainers.mockReturnValue(containerCatalog([], { error: "unavailable" }));
    mocks.listFirmwareFiles.mockResolvedValue([rigFile, hashboardFile]);
    render(page());

    await screen.findByText(rigFile.filename);
    expect(section("Proto Containers").getByText(hashboardFile.filename)).toBeInTheDocument();
  });

  it("keep the section for a user who cannot list containers", async () => {
    setPermissions(["miner:firmware_update", "controller:software_update"]);
    mocks.useContainers.mockReturnValue(containerCatalog([]));
    render(page());

    expect(await section("Proto Containers").findByText("No controller software uploaded")).toBeInTheDocument();
    expect(mocks.useContainers).toHaveBeenCalledWith([], false, false);
  });
});
