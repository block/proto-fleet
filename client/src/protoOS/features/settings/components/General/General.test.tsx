import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import General from "./General";
import { useSystemTag } from "@/protoOS/api";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetFlowUnit, useSystemInfo } from "@/protoOS/store";
import { AUTH_ACTIONS } from "@/protoOS/store/types";

const { mockCheckAccess, mockSetPausedAuthAction, mockState } = vi.hoisted(() => ({
  mockCheckAccess: vi.fn(),
  mockSetPausedAuthAction: vi.fn(),
  mockState: {
    hasAccess: undefined as boolean | undefined,
    pausedAuthAction: null as string | null,
    dismissedLoginModal: false,
  },
}));

// Stub the edit modal so the test can assert it opened via its `open` prop.
vi.mock("./MinerSystemTagEditModal", () => ({
  default: ({ open }: { open: boolean }) => (open ? <div data-testid="tag-edit-modal" /> : null),
}));

vi.mock("@/protoOS/features/firmwareUpdate/components/CheckForUpdate", () => ({
  default: () => null,
}));

vi.mock("@/protoOS/api", () => ({
  useSystemTag: vi.fn(),
  useNetworkInfo: () => ({ data: { ipAddress: "192.168.1.100" }, pending: false }),
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

// The cooling section's dry cooler hold reads through useAuthRetry; its token refresh is not exercised here.
vi.mock("@/protoOS/store/hooks/useAuth", () => ({ useAuthErrors: () => ({ handleAuthErrors: vi.fn() }) }));
vi.mock("@/protoOS/store", () => ({
  useAccessToken: vi.fn(() => ({ checkAccess: mockCheckAccess, hasAccess: mockState.hasAccess })),
  useDismissedLoginModal: vi.fn(() => mockState.dismissedLoginModal),
  useIsProtoRig: vi.fn(() => true),
  usePausedAuthAction: vi.fn(() => mockState.pausedAuthAction),
  useSetDismissedLoginModal: vi.fn(() => vi.fn()),
  useSetPausedAuthAction: vi.fn(() => mockSetPausedAuthAction),
  useSetTemperatureUnit: vi.fn(() => vi.fn()),
  useSetTheme: vi.fn(() => vi.fn()),
  useSystemInfo: vi.fn(() => ({ identity: { productName: "Rig Alpha" }, operatingSystem: { version: "1.2.3" } })),
  useTemperatureUnit: vi.fn(() => "C"),
  useFlowUnit: vi.fn(() => "GPM"),
  useSetFlowUnit: vi.fn(() => vi.fn()),
  useTheme: vi.fn(() => "dark"),
}));

describe("General system-tag flow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useSystemInfo).mockReturnValue({
      identity: { productName: "Rig Alpha" },
      operatingSystem: { version: "1.2.3" },
    } as never);
    mockState.hasAccess = undefined;
    mockState.pausedAuthAction = null;
    mockState.dismissedLoginModal = false;
    (useMinerHosting as Mock).mockReturnValue({ mode: "direct", isFleetHosted: false });
    // Resolve an existing tag so the "Edit" affordance renders.
    (useSystemTag as Mock).mockReturnValue({
      systemTag: "rig-01",
      getSystemTag: vi.fn(),
    });
  });

  test("direct mode gates the edit modal behind the auth check", () => {
    const { getByTestId, queryByTestId } = render(<General />);

    fireEvent.click(getByTestId("edit-details-button"));

    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(AUTH_ACTIONS.systemTag);
    expect(mockCheckAccess).toHaveBeenCalledTimes(1);
    expect(queryByTestId("tag-edit-modal")).not.toBeInTheDocument();
    expect(queryByTestId("flow-button")).not.toBeInTheDocument();
  });

  test("fleet-hosted mode opens the edit modal directly without the auth gate", () => {
    (useMinerHosting as Mock).mockReturnValue({ mode: "fleet", isFleetHosted: true });

    const { getByTestId } = render(<General />);

    fireEvent.click(getByTestId("edit-details-button"));

    expect(getByTestId("tag-edit-modal")).toBeInTheDocument();
    expect(mockSetPausedAuthAction).not.toHaveBeenCalledWith(AUTH_ACTIONS.systemTag);
    expect(mockCheckAccess).not.toHaveBeenCalled();
  });

  test("container reuses preferences and shows its identity and network without rig-only actions", () => {
    vi.mocked(useSystemInfo).mockReturnValue({
      identity: {
        deviceType: "container",
        deviceId: "device:CT1-01",
        productName: "Proto Container",
        serialNumber: "controller-serial",
      },
      operatingSystem: { version: "2.0.0" },
    } as never);
    const setFlowUnit = vi.fn();
    vi.mocked(useSetFlowUnit).mockReturnValue(setFlowUnit);
    const { getByText, getByTestId, queryByTestId, getByRole, queryByRole } = render(<General />);
    expect(getByText("Container ID")).toBeInTheDocument();
    expect(getByText("CT1-01")).toBeInTheDocument();
    expect(getByText("192.168.1.100")).toBeInTheDocument();
    expect(getByText("Control server 1")).toBeInTheDocument();
    expect(getByText("Control server 2").parentElement).toHaveTextContent("—");
    expect(getByText("Last updated").parentElement).toHaveTextContent("—");
    expect(getByTestId("theme-button")).toBeInTheDocument();
    expect(getByTestId("temperature-button")).toBeInTheDocument();
    expect(queryByTestId("rig-product-image")).not.toBeInTheDocument();
    expect(queryByTestId("edit-details-button")).not.toBeInTheDocument();
    expect(queryByTestId("add-miner-id")).not.toBeInTheDocument();
    expect(getByTestId("flow-button")).toHaveTextContent("Gallons per minute");
    fireEvent.click(getByTestId("flow-button"));
    expect(getByRole("dialog", { name: "Flow" })).toBeInTheDocument();
    expect(getByText("US gallons per minute (GPM)")).toBeInTheDocument();
    fireEvent.click(getByText("Liters per minute (L/min)"));
    expect(setFlowUnit).toHaveBeenCalledWith("L/min");
    fireEvent.click(getByRole("button", { name: "Done" }));
    expect(queryByRole("dialog", { name: "Flow" })).not.toBeInTheDocument();
  });
  test.each([false, true])("local hashboard firmware is shown only outside Fleet (hosted=%s)", (isFleetHosted) => {
    vi.mocked(useSystemInfo).mockReturnValue({
      identity: { deviceType: "container", deviceId: "ct1-local" },
      operatingSystem: { version: "2.0.0" },
    } as never);
    vi.mocked(useMinerHosting).mockReturnValue({ isFleetHosted, adapter: { hashboardFirmware: {} } } as never);
    const { queryByRole } = render(<General />);
    expect(!!queryByRole("button", { name: "Upload hashboard firmware" })).toBe(!isFleetHosted);
  });
  test.each([false, true])(
    "local controller software update is shown only outside Fleet (hosted=%s)",
    (isFleetHosted) => {
      vi.mocked(useSystemInfo).mockReturnValue({
        identity: { deviceType: "container", deviceId: "ct1-local" },
        operatingSystem: { version: "2.0.0" },
      } as never);
      const frigateServerUpdate = { status: vi.fn().mockResolvedValue({ servedByMaintenance: false }) };
      vi.mocked(useMinerHosting).mockReturnValue({ isFleetHosted, adapter: { frigateServerUpdate } } as never);
      const { queryByRole } = render(<General />);
      expect(!!queryByRole("button", { name: "Update controller software" })).toBe(!isFleetHosted);
      expect(frigateServerUpdate.status).toHaveBeenCalledTimes(isFleetHosted ? 0 : 1);
    },
  );
});
