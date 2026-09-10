import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import MinersPage from "./MinersWrapper";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { NetworkInfoSchema } from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { type FleetNodeItem, useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { useHasPermission } from "@/protoFleet/store";

vi.mock("@/protoFleet/api/useFleetNodes");
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

beforeEach(() => {
  vi.clearAllMocks();
  mockDiscover.mockReset().mockResolvedValue(undefined);

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
  });
  vi.mocked(useHasPermission).mockReturnValue(true);
  mockListFleetNodes.mockResolvedValue([]);

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
    controlStreamConnected: true,
    createdAt: null,
    lastSeenAt: null,
    ...overrides,
  };
}

describe("MinersWrapper", () => {
  describe("network scan discovery", () => {
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
        expect(screen.getByText("Finding miners on your network")).toBeInTheDocument();
      });
      expect(mockDiscover).toHaveBeenCalled();
      expect(mockDiscover).toHaveBeenCalledWith(
        expect.objectContaining({
          discoverRequest: expect.objectContaining({
            mode: expect.objectContaining({
              case: "nmap",
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
      expect(screen.queryByText("Finding miners on your network")).not.toBeInTheDocument();
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
      // Arrange
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

      // Act
      const findMinersButton = screen.getByTestId("section-scan-network").querySelector("button")!;
      fireEvent.click(findMinersButton);

      // Assert: miners appear in the list while scan is still running
      await waitFor(() => {
        expect(screen.getByText("Finding miners on your network... 2 found so far")).toBeInTheDocument();
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
              case: "nmap",
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

      renderMinersPage("pairing");
      fireEvent.change(screen.getByTestId("ipAddresses"), {
        target: { value: "192.168.1.100\n192.168.1.0/24\n192.168.1.150 - 192.168.1.160" },
      });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);

      await waitFor(() => expect(mockDiscover).toHaveBeenCalledTimes(3));
      const skeleton = screen.getAllByTestId("skeleton-row")[0].parentElement?.parentElement;
      await waitFor(() => expect(skeleton).toHaveStyle({ opacity: "0" }), { timeout: 3000 });
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

    it("shows no topology or warning when every confirmed node is eligible", async () => {
      mockListFleetNodes.mockResolvedValue([fleetNode()]);

      renderMinersPage("pairing");

      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalled());
      expect(screen.queryByTestId("remote-discovery-warning")).not.toBeInTheDocument();
      expect(screen.queryByText("hidden-node-name")).not.toBeInTheDocument();
      expect(screen.queryByText("hidden-fingerprint")).not.toBeInTheDocument();
    });

    it("warns generically when some confirmed nodes are ineligible", async () => {
      mockListFleetNodes.mockResolvedValue([
        fleetNode(),
        fleetNode({ fleetNodeId: "2", controlStreamConnected: false }),
      ]);

      renderMinersPage("pairing");

      expect(
        await screen.findByText("Some remote networks are currently unavailable and may not be searched."),
      ).toBeInTheDocument();
      expect(screen.queryByText("hidden-node-name")).not.toBeInTheDocument();
    });

    it("warns generically when no confirmed node is eligible", async () => {
      mockListFleetNodes.mockResolvedValue([fleetNode({ commandProtocolUpgradeRequired: true })]);

      renderMinersPage("pairing");

      expect(
        await screen.findByText(
          "Remote network discovery is currently unavailable. Some networks may not be searched.",
        ),
      ).toBeInTheDocument();
    });

    it("warns when remote coverage cannot be checked", async () => {
      mockListFleetNodes.mockRejectedValue(new Error("request failed"));

      renderMinersPage("pairing");

      expect(
        await screen.findByText("Remote network availability could not be checked. Some networks may not be searched."),
      ).toBeInTheDocument();
    });

    it("refreshes disconnected and reconnected coverage before manual discovery and rescan", async () => {
      // Arrange
      mockListFleetNodes
        .mockResolvedValueOnce([fleetNode()])
        .mockResolvedValueOnce([fleetNode({ controlStreamConnected: false })])
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
      expect(await screen.findByText("1 miners found on your network")).toBeInTheDocument();
      expect(
        await screen.findByText(
          "Remote network discovery is currently unavailable. Some networks may not be searched.",
        ),
      ).toBeInTheDocument();

      await waitFor(() => expect(screen.getByTestId("add-miners-rescan-network")).toBeEnabled());
      fireEvent.click(screen.getByTestId("add-miners-rescan-network"));
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(3));
      expect(screen.queryByTestId("remote-discovery-warning")).not.toBeInTheDocument();
      expect(mockDiscover).toHaveBeenCalledTimes(2);
    });

    it("shows delayed coverage warnings during scanning and keeps them alongside results", async () => {
      // Arrange
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
      expect(screen.getByText("Finding miners on your network... 1 found so far")).toBeInTheDocument();
      expect(screen.queryByTestId("remote-discovery-warning")).not.toBeInTheDocument();
      await act(async () => resolveCoverage([fleetNode(), fleetNode({ controlStreamConnected: false })]));

      // Assert: delayed warnings are visible without leaving scanning or results.
      const warning = "Some remote networks are currently unavailable and may not be searched.";
      expect(screen.getByText("Finding miners on your network... 1 found so far")).toBeInTheDocument();
      await waitFor(() => expect(screen.getByText(warning)).toBeVisible());
      await act(async () => finishScan());
      expect(screen.getByText("1 miners found on your network")).toBeInTheDocument();
      expect(screen.getByText(warning)).toBeVisible();
      expect(screen.queryByTestId("section-search-by-ip")).not.toBeInTheDocument();
    });

    it("recovers from a failed coverage check on automatic discovery without blocking the scan", async () => {
      // Arrange
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
      expect(await screen.findByTestId("remote-discovery-warning")).toBeInTheDocument();

      // Act: discovery starts while its refreshed health request is still pending.
      fireEvent.click(screen.getByTestId("section-scan-network").querySelector("button")!);

      // Assert
      expect(mockListFleetNodes).toHaveBeenCalledTimes(2);
      expect(mockDiscover).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId("remote-discovery-warning")).toBeVisible();
      await act(async () => resolveCoverage([fleetNode()]));
      expect(screen.queryByTestId("remote-discovery-warning")).not.toBeInTheDocument();
    });

    it("continues discovery when the refreshed coverage check fails", async () => {
      // Arrange
      mockListFleetNodes.mockResolvedValueOnce([fleetNode()]).mockRejectedValueOnce(new Error("request failed"));
      renderMinersPage("pairing");
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(1));

      // Act
      fireEvent.change(screen.getByTestId("ipAddresses"), { target: { value: "192.168.1.100" } });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(2));

      // Assert
      expect(mockDiscover).toHaveBeenCalledTimes(1);
      expect(
        await screen.findByText("Remote network availability could not be checked. Some networks may not be searched."),
      ).toBeInTheDocument();
    });

    it.each(["success", "failure"])("ignores a stale initial coverage %s after a newer scan check", async (outcome) => {
      // Arrange
      let resolveInitial!: (nodes: FleetNodeItem[]) => void;
      let rejectInitial!: (error: Error) => void;
      mockListFleetNodes.mockImplementationOnce(
        () =>
          new Promise<FleetNodeItem[]>((resolve, reject) => {
            resolveInitial = resolve;
            rejectInitial = reject;
          }),
      );
      mockListFleetNodes.mockResolvedValue([fleetNode({ controlStreamConnected: false })]);
      renderMinersPage("pairing");

      // Act
      fireEvent.change(screen.getByTestId("ipAddresses"), { target: { value: "192.168.1.100" } });
      fireEvent.click(screen.getByTestId("section-search-by-ip").querySelector("button")!);
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(2));
      await act(async () => {
        if (outcome === "success") resolveInitial([fleetNode()]);
        else rejectInitial(new Error("stale failure"));
      });

      // Assert: neither an older success nor failure overwrites the latest check.
      expect(
        await screen.findByText(
          "Remote network discovery is currently unavailable. Some networks may not be searched.",
        ),
      ).toBeInTheDocument();
    });

    it("shows no warning when there are no confirmed nodes", async () => {
      mockListFleetNodes.mockResolvedValue([
        fleetNode({ enrollmentStatus: FleetNodeEnrollmentStatus.PENDING, controlStreamConnected: false }),
      ]);

      renderMinersPage("pairing");

      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalled());
      expect(screen.queryByTestId("remote-discovery-warning")).not.toBeInTheDocument();
    });

    it("does not load or show node coverage without miner pairing permission", () => {
      vi.mocked(useHasPermission).mockReturnValue(false);

      renderMinersPage("pairing");

      expect(useHasPermission).toHaveBeenCalledWith("miner:pair");
      expect(mockListFleetNodes).not.toHaveBeenCalled();
      expect(screen.queryByTestId("remote-discovery-warning")).not.toBeInTheDocument();
    });

    it("hides loaded coverage state when miner pairing permission is removed", async () => {
      mockListFleetNodes.mockResolvedValue([fleetNode({ controlStreamConnected: false })]);
      const view = renderMinersPage("pairing");
      expect(await screen.findByTestId("remote-discovery-warning")).toBeInTheDocument();

      vi.mocked(useHasPermission).mockReturnValue(false);
      view.rerender(
        <MemoryRouter>
          <MinersPage mode="pairing" />
        </MemoryRouter>,
      );

      expect(screen.queryByTestId("remote-discovery-warning")).not.toBeInTheDocument();
    });
  });
});
