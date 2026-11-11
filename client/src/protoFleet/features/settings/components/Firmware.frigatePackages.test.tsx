import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Firmware from "./Firmware";
import { useFleetStore } from "@/protoFleet/store";

const mocks = vi.hoisted(() => ({ listFirmwareFiles: vi.fn(), listPackages: vi.fn() }));

vi.mock("@/protoFleet/api/useFirmwareApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFirmwareApi")>()),
  useFirmwareApi: () => ({ listFirmwareFiles: mocks.listFirmwareFiles }),
}));
vi.mock("@/protoFleet/api/useFrigatePackages", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFrigatePackages")>()),
  useFrigatePackages: () => ({ listPackages: mocks.listPackages, deletePackage: vi.fn(), uploadPackage: vi.fn() }),
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

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listFirmwareFiles.mockResolvedValue([]);
  mocks.listPackages.mockResolvedValue([]);
});
afterEach(() => useFleetStore.setState({ auth: initialAuth }));

describe("Firmware Frigate packages tab", () => {
  it("is hidden without the software update permission", async () => {
    setPermissions(["firmware:read"]);
    render(page());
    await screen.findByText("No firmware files uploaded");
    expect(screen.queryByRole("button", { name: "Frigate packages" })).not.toBeInTheDocument();
  });

  it("ignores a deep link to the tab without the permission", async () => {
    setPermissions([]);
    render(page("frigate-packages"));
    await screen.findByText("No firmware files uploaded");
    expect(screen.queryByText("No Frigate packages uploaded")).not.toBeInTheDocument();
    expect(mocks.listPackages).not.toHaveBeenCalled();
  });

  it("shows the packages list from the tab with the permission", async () => {
    setPermissions(["miner:firmware_update", "controller:software_update"]);
    render(page());
    await screen.findByText("No firmware files uploaded");

    fireEvent.click(screen.getByRole("button", { name: "Frigate packages" }));

    expect(await screen.findByText("No Frigate packages uploaded")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Upload Frigate package" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Upload firmware" })).not.toBeInTheDocument();
  });

  it("opens directly on the tab with the permission and returns to files", async () => {
    setPermissions(["miner:firmware_update", "controller:software_update"]);
    render(page("frigate-packages"));
    expect(await screen.findByText("No Frigate packages uploaded")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Files" }));

    await waitFor(() => expect(screen.getByText("No firmware files uploaded")).toBeInTheDocument());
  });

  it("shows only the packages to a user who cannot manage miner firmware", async () => {
    setPermissions(["controller:software_update"]);
    render(page());

    expect(await screen.findByText("No Frigate packages uploaded")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Frigate packages" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Files" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Release channels" })).not.toBeInTheDocument();
    expect(mocks.listFirmwareFiles).not.toHaveBeenCalled();
  });
});
