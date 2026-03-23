import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import MinersPage from "./MinersWrapper";
import { NetworkInfoSchema } from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
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
