import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import MinersPage from "./MinersWrapper";
import { NetworkInfoSchema } from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";

vi.mock("@/protoFleet/api/useMinerPairing");
vi.mock("@/protoFleet/api/useNetworkInfo");
vi.mock("@/protoFleet/api/useOnboardedStatus");

vi.mock("@/protoFleet/store", () => ({
  useMinerIds: vi.fn(() => []),
  useNotifyPairingCompleted: vi.fn(() => vi.fn()),
  useAuthErrors: vi.fn(() => ({ handleAuthErrors: vi.fn() })),
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

beforeEach(() => {
  vi.clearAllMocks();

  vi.mocked(useMinerPairing).mockReturnValue({
    discover: mockDiscover,
    discoverPending: false,
    pairingPending: false,
    pair: mockPair,
  });

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
          createDiscoveredMiner("miner-1-443", "192.168.1.101"),
          createDiscoveredMiner("miner-1-8080", "192.168.1.101"),
          createDiscoveredMiner("miner-2-443", "192.168.1.102"),
          createDiscoveredMiner("miner-2-8080", "192.168.1.102"),
          createDiscoveredMiner("miner-3-443", "192.168.1.103"),
          createDiscoveredMiner("miner-3-8080", "192.168.1.103"),
          createDiscoveredMiner("miner-4-443", "192.168.1.104"),
          createDiscoveredMiner("miner-4-8080", "192.168.1.104"),
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
  });
});
