import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import MinersPage from "./MinersWrapper";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import {
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import {
  CompleteImportResponseSchema,
  ImportFromForemanResponseSchema,
} from "@/protoFleet/api/generated/foremanimport/v1/foremanimport_pb";
import { NetworkInfoSchema } from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { type FleetNodeItem, useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { useForemanImport } from "@/protoFleet/api/useForemanImport";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { useHasPermission } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/protoFleet/api/useFleetNodes");
vi.mock("@/protoFleet/api/useForemanImport");
vi.mock("@/protoFleet/api/useMinerPairing");
vi.mock("@/protoFleet/api/useNetworkInfo");
vi.mock("@/protoFleet/api/useOnboardedStatus");

vi.mock("@/protoFleet/store", () => ({
  useMinerIds: vi.fn(() => []),
  useNotifyPairingCompleted: vi.fn(() => vi.fn()),
  useAuthErrors: vi.fn(() => ({ handleAuthErrors: vi.fn() })),
  useHasPermission: vi.fn(),
  useFleetStore: Object.assign(
    (selector: any) => {
      const state = {
        fleet: {
          refetchMiners: vi.fn(),
          notifyPairingCompleted: vi.fn(),
        },
      };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ fleet: { refetchMiners: vi.fn() } }) },
  ),
}));

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: vi.fn(() => vi.fn()),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(() => 1),
  removeToast: vi.fn(),
  STATUSES: { loading: "loading", error: "error", success: "success" },
}));

const mockDiscover = vi.fn().mockResolvedValue(undefined);
const mockPair = vi.fn();
const mockListFleetNodes = vi.fn().mockResolvedValue([]);
const mockImportFromForeman = vi.fn();
const mockCompleteImport = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  mockDiscover.mockReset().mockResolvedValue(undefined);
  mockPair.mockReset();
  mockImportFromForeman.mockReset();
  mockCompleteImport.mockReset();
  vi.mocked(useForemanImport).mockReturnValue({
    importPending: false,
    importFromForeman: mockImportFromForeman,
    completeImport: mockCompleteImport,
  });

  vi.mocked(useMinerPairing).mockReturnValue({
    discover: mockDiscover,
    discoverPending: false,
    pairingPending: false,
    pair: mockPair,
  });
  vi.mocked(useFleetNodes).mockReturnValue({
    listFleetNodes: mockListFleetNodes,
    createEnrollmentCode: vi.fn(),
    confirmFleetNode: vi.fn(),
    revokeFleetNode: vi.fn(),
    listFleetNodeDevices: vi.fn(),
    listFleetNodeDiscoveredDevices: vi.fn(),
    discoverOnFleetNode: vi.fn(),
    pairDiscoveredDevicesOnFleetNode: vi.fn(),
  });
  vi.mocked(useHasPermission).mockReturnValue(true);
  mockListFleetNodes.mockReset().mockResolvedValue([]);

  vi.mocked(useOnboardedStatus).mockReturnValue({
    poolConfigured: false,
    devicePaired: false,
    statusLoaded: true,
    refetch: vi.fn().mockResolvedValue(null),
  });
});

function renderMinersPage(mode: "onboarding" | "pairing" = "onboarding") {
  return render(
    <MemoryRouter>
      <MinersPage mode={mode} />
    </MemoryRouter>,
  );
}

// Fake only setTimeout, which holds the scan loading state. jsdom starts one
// setInterval to run animation frames. If it starts that interval while
// setInterval is fake, the interval stops when the real timers come back, and
// no animation frame runs in the later tests of this file.
function fakeTimeoutsOnly() {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
}

// Use these helpers after fakeTimeoutsOnly().
async function advanceTimers(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

// The scan loading state stays for 2 s (MIN_LOADING_TIME in Miners.tsx) after discovery ends.
async function advancePastMinLoadingTime() {
  await advanceTimers(0);
  await advanceTimers(2000);
}

function createDiscoveredMiner(deviceIdentifier: string, ipAddress: string) {
  return create(DeviceSchema, {
    deviceIdentifier,
    ipAddress,
    model: "Proto Rig",
    manufacturer: "Proto",
  });
}

function fleetNode(overrides: Partial<FleetNodeItem> = {}): FleetNodeItem {
  return {
    fleetNodeId: "1",
    pendingEnrollmentId: null,
    name: "hidden-node-name",
    enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
    identityFingerprint: "hidden-fingerprint",
    commandProtocolUpgradeRequired: false,
    commandProtocolUpgradeRecommended: false,
    controlStreamConnected: true,
    discoveryCoverageDegraded: false,
    pairedDeviceCount: 0,
    discoveryConfigReported: true,
    discoveryRanges: ["10.1.0.0/20"],
    haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
    haPeerNodeId: null,
    haPeerNodeName: "",
    haStandbyReady: false,
    haStatusReason: "",
    ipAddresses: [],
    version: "",
    enrolledBy: "",
    createdAt: null,
    lastSeenAt: null,
    ...overrides,
  };
}

describe("MinersWrapper", () => {
  describe("Foreman import", () => {
    beforeEach(() => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      mockImportFromForeman.mockImplementation(async ({ onSuccess }) => {
        onSuccess(
          create(ImportFromForemanResponseSchema, {
            miners: [{ ipAddress: "192.168.1.100" }, { ipAddress: "192.168.1.101" }],
          }),
        );
      });
      mockDiscover.mockImplementation(async ({ onStreamData }) => {
        onStreamData([
          createDiscoveredMiner("miner-1", "192.168.1.100"),
          createDiscoveredMiner("miner-2", "192.168.1.101"),
        ]);
      });
      mockCompleteImport.mockImplementation(async ({ onSuccess }) => {
        onSuccess(create(CompleteImportResponseSchema, { groupsCreated: 1, racksCreated: 1 }));
      });
    });

    async function discoverForemanMiners(mode: "onboarding" | "pairing") {
      renderMinersPage(mode);
      if (mode === "onboarding") fireEvent.click(screen.getByText("Get started"));
      fireEvent.click(screen.getByRole("button", { name: "Connect Foreman" }));
      fireEvent.change(screen.getByLabelText("API key"), { target: { value: "foreman-api-key" } });
      fireEvent.change(screen.getByLabelText("Client ID"), { target: { value: "123" } });
      fireEvent.click(screen.getByRole("button", { name: "Import" }));
      await screen.findByRole("button", { name: "Continue with 2 miners" });
    }

    it("explains before importing that pools need separate configuration because Foreman lacks passwords", async () => {
      renderMinersPage("pairing");
      await waitFor(() =>
        expect(screen.getByText("Import miners, groups, racks, and names from Foreman.")).toBeVisible(),
      );
      fireEvent.click(screen.getByRole("button", { name: "Connect Foreman" }));

      await waitFor(() =>
        expect(
          screen.getByText(
            "Import miners and their details. Configure pools separately in Fleet; Foreman does not provide pool passwords.",
          ),
        ).toBeVisible(),
      );
    });

    it.each(["onboarding", "pairing"] as const)(
      "imports groups, racks, and names without pools after successful %s pairing",
      async (mode) => {
        mockPair.mockImplementation(async ({ onSuccess }) => onSuccess([]));
        await discoverForemanMiners(mode);

        fireEvent.click(screen.getByRole("button", { name: "Continue with 2 miners" }));

        await waitFor(() =>
          expect(mockCompleteImport).toHaveBeenCalledWith(
            expect.objectContaining({
              apiKey: "foreman-api-key",
              clientId: "123",
              importPools: false,
              importGroups: true,
              importRacks: true,
              pairedDeviceIdentifiers: ["miner-1", "miner-2"],
            }),
          ),
        );
        expect(pushToast).not.toHaveBeenCalledWith(expect.objectContaining({ status: "error" }));
      },
    );

    it("limits imported Foreman details to miners that paired successfully", async () => {
      mockPair.mockImplementation(async ({ onSuccess }) => onSuccess(["miner-2"]));
      await discoverForemanMiners("pairing");

      fireEvent.click(screen.getByRole("button", { name: "Continue with 2 miners" }));

      await waitFor(() =>
        expect(mockCompleteImport).toHaveBeenCalledWith(
          expect.objectContaining({
            importPools: false,
            importGroups: true,
            importRacks: true,
            pairedDeviceIdentifiers: ["miner-1"],
          }),
        ),
      );
    });

    it("skips Foreman completion when no selected miners pair successfully", async () => {
      mockPair.mockImplementation(async ({ onSuccess }) => onSuccess(["miner-1", "miner-2"]));
      await discoverForemanMiners("pairing");

      fireEvent.click(screen.getByRole("button", { name: "Continue with 2 miners" }));

      await waitFor(() =>
        expect(pushToast).toHaveBeenCalledWith({
          message: "Failed to add 2 miners. Please check that miners are online and try again.",
          status: "error",
        }),
      );
      expect(mockCompleteImport).not.toHaveBeenCalled();
    });

    it("reports a details import failure without claiming it attempted to import pools", async () => {
      const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
      try {
        mockPair.mockImplementation(async ({ onSuccess }) => onSuccess([]));
        mockCompleteImport.mockImplementation(async ({ onError }) => onError("Foreman unavailable"));
        await discoverForemanMiners("pairing");

        fireEvent.click(screen.getByRole("button", { name: "Continue with 2 miners" }));

        await waitFor(() =>
          expect(pushToast).toHaveBeenCalledWith({
            message: "Miners added but failed to import groups, racks, and names from Foreman.",
            status: "error",
          }),
        );
      } finally {
        consoleError.mockRestore();
      }
    });
  });

  describe("network scan discovery", () => {
    describe("already-paired devices", () => {
      beforeEach(() => {
        vi.mocked(useNetworkInfo).mockReturnValue({
          data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
          pending: false,
          error: undefined,
          fetchData: vi.fn(),
          updateNetworkInfo: vi.fn(),
        });
      });

      it("excludes server-marked paired rigs and containers from Add Miners", async () => {
        mockDiscover.mockImplementationOnce(async ({ onStreamData }) => {
          onStreamData([
            create(DeviceSchema, {
              deviceIdentifier: "paired-rig-on-later-page",
              ipAddress: "192.168.1.100",
              model: "Proto Rig",
              deviceClass: DeviceClass.RIG,
              alreadyPaired: true,
            }),
            create(DeviceSchema, {
              deviceIdentifier: "paired-container-controller",
              ipAddress: "192.168.1.101",
              model: "CT1",
              deviceClass: DeviceClass.CONTAINER,
              alreadyPaired: true,
            }),
            create(DeviceSchema, {
              deviceIdentifier: "new-rig",
              ipAddress: "192.168.1.102",
              model: "Proto Rig",
              deviceClass: DeviceClass.RIG,
            }),
            create(DeviceSchema, {
              deviceIdentifier: "second-new-rig",
              ipAddress: "192.168.1.103",
              model: "Proto Rig",
              deviceClass: DeviceClass.RIG,
            }),
          ]);
        });

        renderMinersPage("pairing");
        fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

        expect(await screen.findByText("2 miners found on your network")).toBeInTheDocument();
        fireEvent.click(screen.getByTestId("add-miners-choose-miners"));
        expect(await screen.findByText("192.168.1.102")).toBeInTheDocument();
        expect(screen.getByText("192.168.1.103")).toBeInTheDocument();
        expect(screen.queryByText("192.168.1.100")).not.toBeInTheDocument();
        expect(screen.queryByText("192.168.1.101")).not.toBeInTheDocument();
      });

      it("keeps server-marked devices in onboarding discovery", async () => {
        mockDiscover.mockImplementationOnce(async ({ onStreamData }) => {
          onStreamData([
            create(DeviceSchema, {
              deviceIdentifier: "existing-device",
              ipAddress: "192.168.1.100",
              model: "Proto Rig",
              alreadyPaired: true,
            }),
          ]);
        });

        renderMinersPage("onboarding");
        fireEvent.click(screen.getByText("Get started"));
        fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

        expect(await screen.findByText("1 miner found on your network")).toBeInTheDocument();
        expect(screen.getByTestId("add-miners-continue")).toHaveTextContent("Continue with 1 miners");
      });

      it("removes a retained result when a rescan reports that it is now paired", async () => {
        mockDiscover
          .mockImplementationOnce(async ({ onStreamData }) => {
            onStreamData([
              createDiscoveredMiner("became-paired", "192.168.1.100"),
              createDiscoveredMiner("still-unpaired", "192.168.1.101"),
            ]);
          })
          .mockImplementationOnce(async ({ onStreamData }) => {
            onStreamData([
              create(DeviceSchema, {
                deviceIdentifier: "became-paired",
                ipAddress: "192.168.1.100",
                model: "Proto Rig",
                alreadyPaired: true,
              }),
            ]);
          });

        renderMinersPage("pairing");
        fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);
        expect(await screen.findByText("2 miners found on your network")).toBeInTheDocument();
        await waitFor(() => expect(screen.getByTestId("add-miners-rescan-network")).toBeEnabled());

        fireEvent.click(screen.getByTestId("add-miners-rescan-network"));

        expect(await screen.findByText("1 miner found on your network")).toBeInTheDocument();
        expect(screen.getByTestId("add-miners-continue")).toHaveTextContent("Continue with 1 miners");
      });
    });

    it.each(["network", "manual"])("retains and refreshes found miners on a %s rescan", async (mode) => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      const firstMiner = createDiscoveredMiner("miner-1", "192.168.1.100");
      mockDiscover
        .mockImplementationOnce(async ({ onStreamData }) => {
          onStreamData([firstMiner, createDiscoveredMiner("miner-2", "192.168.1.101")]);
        })
        .mockImplementationOnce(async ({ onStreamData }) => {
          onStreamData([
            createDiscoveredMiner("miner-1", "192.168.1.200"),
            createDiscoveredMiner("miner-3", "192.168.1.201"),
          ]);
        });
      renderMinersPage(mode === "network" ? "onboarding" : "pairing");
      if (mode === "network") {
        fireEvent.click(screen.getByText("Get started"));
        fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);
      } else {
        fireEvent.change(screen.getByTestId("ipAddresses"), { target: { value: "192.168.1.0/24" } });
        fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);
      }
      expect(await screen.findByText("2 miners found on your network")).toBeInTheDocument();
      await waitFor(() => expect(screen.getByTestId("add-miners-rescan-network")).toBeEnabled());

      fireEvent.click(screen.getByTestId("add-miners-rescan-network"));

      expect(await screen.findByText("3 miners found on your network")).toBeInTheDocument();
      fireEvent.click(screen.getByTestId("add-miners-choose-miners"));
      expect(await screen.findByText("192.168.1.200")).toBeInTheDocument();
      expect(screen.queryByText("192.168.1.100")).not.toBeInTheDocument();
      expect(screen.getByText("192.168.1.101")).toBeInTheDocument();
      expect(screen.getByText("192.168.1.201")).toBeInTheDocument();
    });

    it("mounts the found-miners modal only while open and keeps the selection across reopening", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      mockDiscover.mockImplementationOnce(async ({ onStreamData }) => {
        onStreamData([
          createDiscoveredMiner("miner-1", "192.168.1.100"),
          createDiscoveredMiner("miner-2", "192.168.1.101"),
        ]);
      });
      renderMinersPage("onboarding");
      fireEvent.click(screen.getByText("Get started"));
      fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);
      expect(await screen.findByText("2 miners found on your network")).toBeInTheDocument();
      const modalDescription = "Selected devices will be added to your fleet.";

      expect(screen.queryByText(modalDescription)).not.toBeInTheDocument();
      fireEvent.click(screen.getByTestId("add-miners-choose-miners"));
      expect(await screen.findByText(modalDescription)).toBeInTheDocument();
      fireEvent.click(screen.getByText("192.168.1.100").closest("tr")!.querySelector("input[type=checkbox]")!);
      fireEvent.click(screen.getByRole("button", { name: "Done" }));

      await waitFor(() => expect(screen.queryByText(modalDescription)).not.toBeInTheDocument());
      expect(screen.getByTestId("add-miners-continue")).toHaveTextContent("Continue with 1 miners");
      fireEvent.click(screen.getByTestId("add-miners-choose-miners"));
      expect(await screen.findByText("1 device selected")).toBeInTheDocument();
    });

    it("starts fresh after closing and reopening onboarding discovery", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      mockDiscover
        .mockImplementationOnce(async ({ onStreamData }) => {
          onStreamData([createDiscoveredMiner("miner-1", "192.168.1.100")]);
        })
        .mockImplementationOnce(async ({ onStreamData }) => {
          onStreamData([createDiscoveredMiner("miner-2", "192.168.1.200")]);
        });
      renderMinersPage("onboarding");
      fireEvent.click(screen.getByText("Get started"));
      fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);
      expect(await screen.findByText("1 miner found on your network")).toBeInTheDocument();

      fireEvent.click(screen.getByLabelText("Close add miners"));
      fireEvent.click(screen.getByText("Get started"));
      fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

      await waitFor(() => expect(mockDiscover).toHaveBeenCalledTimes(2));
      expect(await screen.findByText("1 miner found on your network")).toBeInTheDocument();
    });

    it("shows an incomplete-discovery notification with the source and keeps discovered miners", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      mockDiscover.mockImplementationOnce(async ({ onStreamData, onWarning }) => {
        onStreamData([createDiscoveredMiner("miner-1", "192.168.1.101")]);
        onWarning("Fleet Node north: scan timed out");
        onStreamData([createDiscoveredMiner("miner-2", "192.168.1.102")]);
      });

      renderMinersPage("onboarding");
      fireEvent.click(screen.getByText("Get started"));
      fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

      await waitFor(() => {
        expect(screen.getByText("2 miners found on your network")).toBeInTheDocument();
      });
      expect(pushToast).toHaveBeenCalledWith({
        message: "Discovery incomplete: Fleet Node north: scan timed out",
        status: "error",
      });
    });

    it("shows loading skeleton when network info is available and Find miners is clicked", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      renderMinersPage("onboarding");

      fireEvent.click(screen.getByText("Get started"));

      const findMinersButton = screen.getByTestId("section-scan-network").querySelector("button")!;
      fireEvent.click(findMinersButton);

      await waitFor(() => {
        expect(screen.getByText("Finding devices on your network")).toBeInTheDocument();
      });
      expect(mockDiscover).toHaveBeenCalled();
      expect(mockDiscover).toHaveBeenCalledWith(
        expect.objectContaining({
          discoverRequest: expect.objectContaining({
            mode: expect.objectContaining({
              case: "networkScan",
              value: expect.objectContaining({
                target: "192.168.1.0/24",
              }),
            }),
          }),
        }),
      );
      const scanRequest = mockDiscover.mock.calls[0][0].discoverRequest;
      expect(scanRequest.mode.value.useFleetNodeLocalSubnet).toBe(true);
      expect(scanRequest.mode.value.ports).toEqual([]);
    });

    it("disables Find miners button while network info is loading", () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: undefined,
        pending: true,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      renderMinersPage("onboarding");

      fireEvent.click(screen.getByText("Get started"));

      const findMinersButton = screen.getByTestId("section-scan-network").querySelector("button")!;
      expect(findMinersButton).toBeDisabled();
    });

    it("does not call discover when networkInfo is not available", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: undefined,
        pending: true,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      renderMinersPage("onboarding");

      fireEvent.click(screen.getByText("Get started"));

      // Button is disabled so clicking should not trigger discovery
      const findMinersButton = screen.getByTestId("section-scan-network").querySelector("button")!;
      fireEvent.click(findMinersButton);

      expect(mockDiscover).not.toHaveBeenCalled();
      // Should stay on findMiners step, not switch to pairing
      expect(screen.queryByText("Finding devices on your network")).not.toBeInTheDocument();
    });

    it("disables Find miners button when networkInfo fetch failed (pending: false, data: undefined)", () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: undefined,
        pending: false,
        error: "fetch failed",
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      renderMinersPage("onboarding");

      fireEvent.click(screen.getByText("Get started"));

      const findMinersButton = screen.getByTestId("section-scan-network").querySelector("button")!;
      expect(findMinersButton).toBeDisabled();
    });

    it("shows discovered miners progressively while scan is still in progress", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      let resolveDiscover!: () => void;
      mockDiscover.mockImplementationOnce(
        ({ onStreamData }: { onStreamData: (devices: ReturnType<typeof createDiscoveredMiner>[]) => void }) =>
          new Promise<void>((resolve) => {
            resolveDiscover = resolve;
            onStreamData([
              createDiscoveredMiner("miner-1", "192.168.1.101"),
              createDiscoveredMiner("miner-2", "192.168.1.102"),
            ]);
          }),
      );

      renderMinersPage("onboarding");
      fireEvent.click(screen.getByText("Get started"));

      const findMinersButton = screen.getByTestId("section-scan-network").querySelector("button")!;
      fireEvent.click(findMinersButton);

      // Assert: miners appear in the list while scan is still running
      await waitFor(() => {
        expect(screen.getByText("Scanning… 2 miners found on your network")).toBeInTheDocument();
      });
      expect(screen.getByRole("button", { name: "Continue with 2 miners" })).toBeInTheDocument();

      resolveDiscover();
    });

    it("renders skeleton rows alongside discovered miners while scan is in progress", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      let resolveDiscover!: () => void;
      mockDiscover.mockImplementationOnce(
        ({ onStreamData }: { onStreamData: (devices: ReturnType<typeof createDiscoveredMiner>[]) => void }) =>
          new Promise<void>((resolve) => {
            resolveDiscover = resolve;
            onStreamData([createDiscoveredMiner("miner-1", "192.168.1.101")]);
          }),
      );

      renderMinersPage("onboarding");
      fireEvent.click(screen.getByText("Get started"));
      const findMinersButton = screen.getByTestId("section-scan-network").querySelector("button")!;
      fireEvent.click(findMinersButton);

      await waitFor(() => {
        expect(screen.getByTestId("found-miners-list")).toBeInTheDocument();
      });

      // Real miner row and skeleton placeholders should both be visible
      expect(screen.getByTestId("miner-model-row")).toBeInTheDocument();
      const skeletonRows = screen.getByTestId("found-miners-list").querySelectorAll('[data-testid="skeleton-row"]');
      expect(skeletonRows.length).toBeGreaterThan(0);

      resolveDiscover();
    });

    it("restores the results panel with skeleton rows when rescanning after an empty scan", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      let resolveRescan!: () => void;
      mockDiscover.mockResolvedValueOnce(undefined).mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveRescan = resolve;
          }),
      );

      fakeTimeoutsOnly();
      try {
        renderMinersPage("onboarding");
        fireEvent.click(screen.getByText("Get started"));
        fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

        await advancePastMinLoadingTime();
        expect(screen.getByText("No devices found")).toBeInTheDocument();
        expect(screen.queryByTestId("found-miners-list")).not.toBeInTheDocument();

        fireEvent.click(screen.getByTestId("add-miners-rescan-network"));
        await advanceTimers(0);

        expect(screen.getByText("Finding devices on your network")).toBeInTheDocument();
        expect(mockDiscover).toHaveBeenCalledTimes(2);
        expect(within(screen.getByTestId("found-miners-list")).getAllByTestId("skeleton-row")).toHaveLength(3);

        resolveRescan();
      } finally {
        vi.useRealTimers();
      }
    });

    it("deduplicates duplicate discoveries in the add-miners UI count", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      mockDiscover.mockImplementationOnce(async ({ onStreamData }) => {
        onStreamData([
          createDiscoveredMiner("miner-1", "192.168.1.101"),
          createDiscoveredMiner("miner-1", "192.168.1.101"),
          createDiscoveredMiner("miner-2", "192.168.1.102"),
          createDiscoveredMiner("miner-2", "192.168.1.102"),
          createDiscoveredMiner("miner-3", "192.168.1.103"),
          createDiscoveredMiner("miner-3", "192.168.1.103"),
          createDiscoveredMiner("miner-4", "192.168.1.104"),
          createDiscoveredMiner("miner-4", "192.168.1.104"),
        ]);
      });

      renderMinersPage("onboarding");

      fireEvent.click(screen.getByText("Get started"));

      const findMinersButton = screen.getByTestId("section-scan-network").querySelector("button")!;
      fireEvent.click(findMinersButton);

      await waitFor(
        () => {
          expect(screen.getByText("4 miners found on your network")).toBeInTheDocument();
        },
        { timeout: 4000 },
      );

      expect(screen.getByRole("button", { name: "Continue with 4 miners" })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Continue with 8 miners" })).not.toBeInTheDocument();
    });

    it("keeps distinct miners that share an IP address across remote networks", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      mockDiscover.mockImplementationOnce(async ({ onStreamData }) => {
        onStreamData([
          createDiscoveredMiner("site-a-miner", "192.168.1.101"),
          createDiscoveredMiner("site-b-miner", "192.168.1.101"),
        ]);
      });

      renderMinersPage("onboarding");
      fireEvent.click(screen.getByText("Get started"));
      fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

      await waitFor(() => expect(screen.getByText("2 miners found on your network")).toBeInTheDocument(), {
        timeout: 4000,
      });
      expect(screen.getByRole("button", { name: "Continue with 2 miners" })).toBeInTheDocument();
    });

    it("keeps discovery results that have no usable identity", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      mockDiscover.mockImplementationOnce(async ({ onStreamData }) => {
        onStreamData([createDiscoveredMiner("", ""), createDiscoveredMiner("", "")]);
      });

      renderMinersPage("onboarding");
      fireEvent.click(screen.getByText("Get started"));
      fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

      await waitFor(() => expect(screen.getByText("2 miners found on your network")).toBeInTheDocument(), {
        timeout: 4000,
      });
    });
  });

  describe("manual discovery", () => {
    it("omits default ports for IPs, subnets, and ranges", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: undefined,
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });

      renderMinersPage("onboarding");

      fireEvent.click(screen.getByText("Get started"));
      fireEvent.change(screen.getByTestId("ipAddresses"), {
        target: {
          value: "192.168.1.100\n192.168.1.0/24\n192.168.1.150 - 192.168.1.160",
        },
      });

      const findMinersButton = screen.getByTestId("section-search-by-ip").querySelector("button")!;
      fireEvent.click(findMinersButton);

      await waitFor(() => {
        expect(mockDiscover).toHaveBeenCalledTimes(3);
      });

      expect(mockDiscover).toHaveBeenNthCalledWith(
        1,
        expect.objectContaining({
          discoverRequest: expect.objectContaining({
            mode: expect.objectContaining({
              case: "ipList",
              value: expect.objectContaining({
                ipAddresses: ["192.168.1.100"],
              }),
            }),
          }),
        }),
      );
      expect(mockDiscover.mock.calls[0][0].discoverRequest.mode.value.ports).toEqual([]);

      expect(mockDiscover).toHaveBeenNthCalledWith(
        2,
        expect.objectContaining({
          discoverRequest: expect.objectContaining({
            mode: expect.objectContaining({
              case: "networkScan",
              value: expect.objectContaining({
                target: "192.168.1.0/24",
              }),
            }),
          }),
        }),
      );
      expect(mockDiscover.mock.calls[1][0].discoverRequest.mode.value.ports).toEqual([]);
      expect(mockDiscover.mock.calls[1][0].discoverRequest.mode.value.useFleetNodeLocalSubnet).toBe(false);

      expect(mockDiscover).toHaveBeenNthCalledWith(
        3,
        expect.objectContaining({
          discoverRequest: expect.objectContaining({
            mode: expect.objectContaining({
              case: "ipRange",
              value: expect.objectContaining({
                startIp: "192.168.1.150",
                endIp: "192.168.1.160",
              }),
            }),
          }),
        }),
      );
      expect(mockDiscover.mock.calls[2][0].discoverRequest.mode.value.ports).toEqual([]);
    });

    it("submits mixed manual requests sequentially", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: undefined,
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      const resolvers: Array<() => void> = [];
      mockDiscover.mockImplementation(
        () =>
          new Promise<void>((resolve) => {
            resolvers.push(resolve);
          }),
      );

      renderMinersPage("pairing");
      fireEvent.change(screen.getByTestId("ipAddresses"), {
        target: { value: "192.168.1.100\n192.168.1.0/24\n192.168.1.150 - 192.168.1.160" },
      });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);

      await waitFor(() => expect(mockDiscover).toHaveBeenCalledTimes(1));
      resolvers[0]();
      await waitFor(() => expect(mockDiscover).toHaveBeenCalledTimes(2));
      resolvers[1]();
      await waitFor(() => expect(mockDiscover).toHaveBeenCalledTimes(3));
      resolvers[2]();
    });

    it("aborts a mixed manual request and does not start the next request after unmount", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: undefined,
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      let resolveFirst!: () => void;
      mockDiscover.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveFirst = resolve;
          }),
      );

      const view = renderMinersPage("pairing");
      fireEvent.change(screen.getByTestId("ipAddresses"), {
        target: { value: "192.168.1.100\n192.168.1.0/24" },
      });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);

      await waitFor(() => expect(mockDiscover).toHaveBeenCalledTimes(1));
      const signal = mockDiscover.mock.calls[0][0].discoverAbortController.signal;
      view.unmount();
      expect(signal.aborted).toBe(true);
      resolveFirst();
      await waitFor(() => expect(mockDiscover).toHaveBeenCalledTimes(1));
    });

    it("continues mixed manual requests after one fails", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: undefined,
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      mockDiscover.mockRejectedValueOnce(new Error("node busy")).mockResolvedValue(undefined);

      fakeTimeoutsOnly();
      try {
        renderMinersPage("pairing");
        fireEvent.change(screen.getByTestId("ipAddresses"), {
          target: { value: "192.168.1.100\n192.168.1.0/24\n192.168.1.150 - 192.168.1.160" },
        });
        fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);

        await advanceTimers(0);
        expect(mockDiscover).toHaveBeenCalledTimes(3);

        await advancePastMinLoadingTime();
        expect(screen.getByText("No devices found")).toBeInTheDocument();
        expect(screen.queryByTestId("found-miners-list")).not.toBeInTheDocument();
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe("remote discovery coverage", () => {
    beforeEach(() => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: undefined,
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
    });

    it.each([false, true])(
      "scans configured Fleet Nodes without a Fleet Server subnet (network request pending: %s)",
      async (pending) => {
        vi.mocked(useNetworkInfo).mockReturnValue({
          data: undefined,
          pending,
          error: undefined,
          fetchData: vi.fn(),
          updateNetworkInfo: vi.fn(),
        });
        mockListFleetNodes.mockResolvedValue([
          fleetNode({
            discoveryConfigReported: true,
            discoveryRanges: ["10.1.0.0/20"],
          }),
        ]);
        renderMinersPage("pairing");
        const scan = within(screen.getByTestId("section-scan-network")).getByRole("button", { name: "Find miners" });
        await waitFor(() => expect(scan).toBeEnabled());
        fireEvent.click(scan);
        await waitFor(() => expect(mockDiscover).toHaveBeenCalledTimes(1));
        expect(mockDiscover.mock.calls[0][0].discoverRequest.mode).toMatchObject({
          case: "networkScan",
          value: { target: "", useFleetNodeLocalSubnet: true },
        });
      },
    );

    it("requires reported discovery ranges before offering a remote-only scan to node readers", async () => {
      mockListFleetNodes.mockResolvedValue([fleetNode({ discoveryConfigReported: false, discoveryRanges: [] })]);
      renderMinersPage("pairing");
      expect(await screen.findByText("Configure Fleet Node discovery ranges")).toBeVisible();
      expect(
        within(screen.getByTestId("section-scan-network")).getByRole("button", { name: "Find miners" }),
      ).toBeDisabled();
    });

    it("allows redacted pairing operators to scan without inventing node metadata", async () => {
      vi.mocked(useHasPermission).mockImplementation((permission) => permission === "miner:pair");
      mockListFleetNodes.mockResolvedValue([
        fleetNode({ name: "", discoveryConfigReported: false, discoveryRanges: [], pairedDeviceCount: 0 }),
      ]);
      mockDiscover.mockImplementation(async ({ onStreamData }) => {
        onStreamData([create(DeviceSchema, { deviceIdentifier: "remote-miner", fleetNodeId: 1n })]);
      });
      renderMinersPage("pairing");
      const scan = within(screen.getByTestId("section-scan-network")).getByRole("button", { name: "Find miners" });
      await waitFor(() => expect(scan).toBeEnabled());
      fireEvent.click(scan);
      expect(await screen.findByRole("button", { name: "Continue with 1 miners" })).toBeVisible();
      expect(screen.queryByText("hidden-node-name")).not.toBeInTheDocument();
    });

    it("shows no topology or warning when every confirmed node is eligible", async () => {
      mockListFleetNodes.mockResolvedValue([fleetNode()]);

      renderMinersPage("pairing");

      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalled());
      expect(screen.queryByTestId("remote-discovery-guidance")).not.toBeInTheDocument();
      expect(screen.queryByText("hidden-node-name")).not.toBeInTheDocument();
      expect(screen.queryByText("hidden-fingerprint")).not.toBeInTheDocument();
      await waitFor(() =>
        expect(
          screen.getByText(/To find miners on a network Fleet can't reach directly, install and enroll Fleet Node/),
        ).toBeVisible(),
      );
    });

    it("does not warn when an offline standby is covered by its active peer", async () => {
      mockListFleetNodes.mockResolvedValue([
        fleetNode(),
        fleetNode({
          fleetNodeId: "2",
          controlStreamConnected: false,
          discoveryConfigReported: false,
          discoveryRanges: [],
          haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
        }),
      ]);

      renderMinersPage("pairing");

      await waitFor(() =>
        expect(
          within(screen.getByTestId("section-scan-network")).getByRole("button", { name: "Find miners" }),
        ).toBeEnabled(),
      );
      expect(screen.queryByTestId("remote-discovery-guidance")).not.toBeInTheDocument();
    });

    it("does not enable node-based scanning through a standby before failover", async () => {
      mockListFleetNodes.mockResolvedValue([
        fleetNode({
          controlStreamConnected: false,
          discoveryCoverageDegraded: true,
          discoveryConfigReported: false,
          discoveryRanges: [],
        }),
        fleetNode({ fleetNodeId: "2", haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY }),
      ]);

      renderMinersPage("pairing");

      expect(await screen.findByText("Some Fleet Nodes can't scan. Results may be incomplete.")).toBeVisible();
      expect(
        within(screen.getByTestId("section-scan-network")).getByRole("button", { name: "Find miners" }),
      ).toBeDisabled();
    });

    it("warns generically when some confirmed nodes are ineligible", async () => {
      mockListFleetNodes.mockResolvedValue([
        fleetNode(),
        fleetNode({ fleetNodeId: "2", controlStreamConnected: false, discoveryCoverageDegraded: true }),
      ]);

      renderMinersPage("pairing");

      expect(await screen.findByText("Some Fleet Nodes can't scan. Results may be incomplete.")).toBeInTheDocument();
      expect(screen.queryByText("hidden-node-name")).not.toBeInTheDocument();
    });

    it("warns generically when no confirmed node is eligible", async () => {
      mockListFleetNodes.mockResolvedValue([
        fleetNode({ commandProtocolUpgradeRequired: true, discoveryCoverageDegraded: true }),
      ]);

      renderMinersPage("pairing");

      expect(await screen.findByText("Some Fleet Nodes can't scan. Results may be incomplete.")).toBeInTheDocument();
    });

    it("hides the settings link without Fleet Node read permission", async () => {
      vi.mocked(useHasPermission).mockImplementation((permission) => permission !== "fleetnode:read");
      mockListFleetNodes.mockRejectedValue(new Error("request failed"));
      renderMinersPage("pairing");
      expect(await screen.findByText("Ask an administrator to check Fleet Nodes.")).toBeVisible();
      expect(screen.queryByRole("link", { name: "Manage Nodes" })).not.toBeInTheDocument();
    });

    it("warns and links to settings when remote coverage cannot be checked", async () => {
      mockListFleetNodes.mockRejectedValue(new Error("request failed"));

      renderMinersPage("pairing");

      expect(await screen.findByText("Couldn't check Fleet Nodes. Results may be incomplete.")).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "Manage Nodes" })).toHaveAttribute("href", "/settings/nodes");
    });

    it("refreshes disconnected and reconnected coverage before manual discovery and rescan", async () => {
      mockListFleetNodes
        .mockResolvedValueOnce([fleetNode()])
        .mockResolvedValueOnce([fleetNode({ controlStreamConnected: false, discoveryCoverageDegraded: true })])
        .mockResolvedValue([fleetNode()]);
      mockDiscover.mockImplementation(async ({ onStreamData }) => {
        onStreamData([createDiscoveredMiner("miner-1", "192.168.1.100")]);
      });
      renderMinersPage("pairing");
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(1));

      // Act: the node disconnects before a manual scan, then reconnects before rescan.
      fireEvent.change(screen.getByTestId("ipAddresses"), { target: { value: "192.168.1.100" } });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(2));

      // Assert: the refreshed disconnected state is visible alongside results.
      expect(await screen.findByText("1 miner found on your network")).toBeInTheDocument();
      expect(await screen.findByText("Some Fleet Nodes can't scan. Results may be incomplete.")).toBeInTheDocument();

      await waitFor(() => expect(screen.getByTestId("add-miners-rescan-network")).toBeEnabled());
      fireEvent.click(screen.getByTestId("add-miners-rescan-network"));
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(3));
      expect(screen.queryByTestId("remote-discovery-guidance")).not.toBeInTheDocument();
      expect(mockDiscover).toHaveBeenCalledTimes(2);
    });

    it("shows delayed coverage warnings during scanning and keeps them alongside results", async () => {
      let resolveCoverage!: (nodes: FleetNodeItem[]) => void;
      let finishScan!: () => void;
      mockListFleetNodes.mockResolvedValueOnce([fleetNode()]).mockImplementationOnce(
        () =>
          new Promise<FleetNodeItem[]>((resolve) => {
            resolveCoverage = resolve;
          }),
      );
      mockDiscover.mockImplementation(({ onStreamData }) => {
        onStreamData([createDiscoveredMiner("miner-1", "192.168.1.100")]);
        return new Promise<void>((resolve) => {
          finishScan = resolve;
        });
      });
      renderMinersPage("pairing");
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(1));

      // Act: start scanning before the coverage refresh finishes.
      fireEvent.change(screen.getByTestId("ipAddresses"), { target: { value: "192.168.1.100" } });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);
      expect(screen.getByText("Scanning… 1 miner found on your network")).toBeInTheDocument();
      expect(screen.queryByTestId("remote-discovery-guidance")).not.toBeInTheDocument();
      await act(async () =>
        resolveCoverage([fleetNode(), fleetNode({ controlStreamConnected: false, discoveryCoverageDegraded: true })]),
      );

      // Assert: delayed warnings are visible without leaving scanning or results.
      const warning = "Some Fleet Nodes can't scan. Results may be incomplete.";
      expect(screen.getByText("Scanning… 1 miner found on your network")).toBeInTheDocument();
      await waitFor(() => expect(screen.getByText(warning)).toBeVisible());
      await act(async () => finishScan());
      expect(screen.getByText("1 miner found on your network")).toBeInTheDocument();
      expect(screen.getByText(warning)).toBeVisible();
      expect(screen.queryByTestId("section-search-by-ip")).not.toBeInTheDocument();
    });

    it("recovers from a failed coverage check on automatic discovery without blocking the scan", async () => {
      vi.mocked(useNetworkInfo).mockReturnValue({
        data: create(NetworkInfoSchema, { subnet: "192.168.1.0/24" }),
        pending: false,
        error: undefined,
        fetchData: vi.fn(),
        updateNetworkInfo: vi.fn(),
      });
      let resolveCoverage!: (nodes: FleetNodeItem[]) => void;
      mockListFleetNodes.mockRejectedValueOnce(new Error("request failed")).mockImplementationOnce(
        () =>
          new Promise<FleetNodeItem[]>((resolve) => {
            resolveCoverage = resolve;
          }),
      );
      renderMinersPage("pairing");
      expect(await screen.findByTestId("remote-discovery-guidance")).toBeInTheDocument();

      // Act: discovery starts while its refreshed health request is still pending.
      fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

      expect(mockListFleetNodes).toHaveBeenCalledTimes(2);
      expect(mockDiscover).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId("remote-discovery-guidance")).toBeVisible();
      await act(async () => resolveCoverage([fleetNode()]));
      expect(screen.queryByTestId("remote-discovery-guidance")).not.toBeInTheDocument();
    });

    it("continues discovery when the refreshed coverage check fails", async () => {
      mockListFleetNodes.mockResolvedValueOnce([fleetNode()]).mockRejectedValueOnce(new Error("request failed"));
      renderMinersPage("pairing");
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(1));

      fireEvent.change(screen.getByTestId("ipAddresses"), { target: { value: "192.168.1.100" } });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(2));

      expect(mockDiscover).toHaveBeenCalledTimes(1);
      expect(await screen.findByText("Couldn't check Fleet Nodes. Results may be incomplete.")).toBeInTheDocument();
    });

    it.each(["success", "failure"])("ignores a stale initial coverage %s after a newer scan check", async (outcome) => {
      let resolveInitial!: (nodes: FleetNodeItem[]) => void;
      let rejectInitial!: (error: Error) => void;
      mockListFleetNodes.mockImplementationOnce(
        () =>
          new Promise<FleetNodeItem[]>((resolve, reject) => {
            resolveInitial = resolve;
            rejectInitial = reject;
          }),
      );
      mockListFleetNodes.mockResolvedValue([
        fleetNode({ controlStreamConnected: false, discoveryCoverageDegraded: true }),
      ]);
      renderMinersPage("pairing");

      fireEvent.change(screen.getByTestId("ipAddresses"), { target: { value: "192.168.1.100" } });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(2));
      await act(async () => {
        if (outcome === "success") resolveInitial([fleetNode()]);
        else rejectInitial(new Error("stale failure"));
      });

      // Assert: neither an older success nor failure overwrites the latest check.
      expect(await screen.findByText("Some Fleet Nodes can't scan. Results may be incomplete.")).toBeInTheDocument();
    });

    it.each([
      ["empty", [], "No Fleet Nodes set up"],
      ["revoked only", [fleetNode({ enrollmentStatus: FleetNodeEnrollmentStatus.REVOKED })], "No Fleet Nodes set up"],
      [
        "pending",
        [fleetNode({ enrollmentStatus: FleetNodeEnrollmentStatus.PENDING })],
        "Complete Fleet Node enrollment",
      ],
      [
        "awaiting confirmation",
        [fleetNode({ enrollmentStatus: FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION })],
        "Complete Fleet Node enrollment",
      ],
      [
        "ready and pending",
        [fleetNode(), fleetNode({ enrollmentStatus: FleetNodeEnrollmentStatus.PENDING })],
        "Complete Fleet Node enrollment",
      ],
    ])("explains the next setup step for %s nodes", async (_label, nodes, message) => {
      mockListFleetNodes.mockResolvedValue(nodes);

      renderMinersPage("pairing");

      expect(await screen.findByText(message as string)).toBeVisible();
      expect(screen.getByRole("link", { name: "Manage Nodes" })).toHaveAttribute("href", "/settings/nodes");
    });

    it("does not require a recommended protocol upgrade before scanning", async () => {
      mockListFleetNodes.mockResolvedValue([fleetNode({ commandProtocolUpgradeRecommended: true })]);
      renderMinersPage("pairing");
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalled());
      expect(screen.queryByTestId("remote-discovery-guidance")).not.toBeInTheDocument();
      await waitFor(() =>
        expect(
          screen.getByText(/To find miners on a network Fleet can't reach directly, install and enroll Fleet Node/),
        ).toBeVisible(),
      );
    });

    it.each([
      ["no", [], "Ask an administrator to set up a node."],
      [
        "pending",
        [fleetNode({ enrollmentStatus: FleetNodeEnrollmentStatus.PENDING })],
        "Ask an administrator to finish enrolling the node.",
      ],
      [
        "ineligible",
        [fleetNode({ controlStreamConnected: false, discoveryCoverageDegraded: true })],
        "Ask an administrator to check Fleet Nodes.",
      ],
    ])(
      "directs an operator without management permission to an administrator for %s nodes",
      async (_label, nodes, hint) => {
        vi.mocked(useHasPermission).mockImplementation((permission) => permission !== "fleetnode:manage");
        mockListFleetNodes.mockResolvedValue(nodes);

        renderMinersPage("pairing");

        expect(await screen.findByText(hint as string)).toBeVisible();
        expect(screen.getByRole("link", { name: "Manage Nodes" })).toHaveAttribute("href", "/settings/nodes");
      },
    );

    it("does not send an operator to an administrator when every node is ready", async () => {
      vi.mocked(useHasPermission).mockImplementation((permission) => permission !== "fleetnode:manage");
      mockListFleetNodes.mockResolvedValue([fleetNode()]);

      renderMinersPage("pairing");
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalled());

      await waitFor(() =>
        expect(
          screen.getByText(/To find miners on a network Fleet can't reach directly, install and enroll Fleet Node/),
        ).toBeVisible(),
      );
      expect(screen.queryByText(/Ask an administrator/)).not.toBeInTheDocument();
    });

    it("omits the administrator hint for an operator who can manage nodes", async () => {
      mockListFleetNodes.mockResolvedValue([]);

      renderMinersPage("pairing");

      expect(await screen.findByText("No Fleet Nodes set up")).toBeVisible();
      expect(screen.queryByText(/Ask an administrator/)).not.toBeInTheDocument();
      expect(screen.getByRole("link", { name: "Manage Nodes" })).toHaveAttribute("href", "/settings/nodes");
    });

    it("keeps setup guidance off the results step", async () => {
      mockListFleetNodes.mockResolvedValue([]);
      mockDiscover.mockImplementation(async ({ onStreamData }) => {
        onStreamData([createDiscoveredMiner("miner-1", "192.168.1.100")]);
      });
      renderMinersPage("pairing");
      expect(await screen.findByText("No Fleet Nodes set up")).toBeVisible();

      fireEvent.change(screen.getByTestId("ipAddresses"), { target: { value: "192.168.1.100" } });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);

      expect(await screen.findByText("1 miner found on your network")).toBeInTheDocument();
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(2));
      expect(screen.queryByTestId("remote-network-prerequisite")).not.toBeInTheDocument();
    });

    it("refreshes readiness on returning to the tab and stops listening after unmount", async () => {
      mockListFleetNodes.mockResolvedValueOnce([]).mockResolvedValue([fleetNode()]);
      const view = renderMinersPage("pairing");
      expect(await screen.findByText("No Fleet Nodes set up")).toBeVisible();
      const visibility = vi.spyOn(document, "visibilityState", "get");
      try {
        visibility.mockReturnValue("hidden");
        fireEvent(document, new Event("visibilitychange"));
        expect(mockListFleetNodes).toHaveBeenCalledTimes(1);
        visibility.mockReturnValue("visible");
        fireEvent(document, new Event("visibilitychange"));
        await waitFor(() => expect(screen.queryByText("No Fleet Nodes set up")).not.toBeInTheDocument());
        expect(mockListFleetNodes).toHaveBeenCalledTimes(2);
        view.unmount();
        fireEvent(document, new Event("visibilitychange"));
        expect(mockListFleetNodes).toHaveBeenCalledTimes(2);
      } finally {
        visibility.mockRestore();
      }
    });

    it("does not load or show node coverage without miner pairing permission", () => {
      vi.mocked(useHasPermission).mockReturnValue(false);

      renderMinersPage("pairing");

      expect(useHasPermission).toHaveBeenCalledWith("miner:pair");
      expect(mockListFleetNodes).not.toHaveBeenCalled();
      expect(screen.queryByTestId("remote-discovery-guidance")).not.toBeInTheDocument();
    });

    it("hides loaded coverage state when miner pairing permission is removed", async () => {
      mockListFleetNodes.mockResolvedValue([
        fleetNode({ controlStreamConnected: false, discoveryCoverageDegraded: true }),
      ]);
      const view = renderMinersPage("pairing");
      expect(await screen.findByTestId("remote-discovery-guidance")).toBeInTheDocument();

      vi.mocked(useHasPermission).mockReturnValue(false);
      view.rerender(
        <MemoryRouter>
          <MinersPage mode="pairing" />
        </MemoryRouter>,
      );

      expect(screen.queryByTestId("remote-discovery-guidance")).not.toBeInTheDocument();
    });
  });
});
