import type { ReactNode } from "react";
import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  useErrors: vi.fn(),
  useFirmwareUpdate: vi.fn(),
  useHardware: vi.fn(),
  useHashboardStatus: vi.fn(),
  useMiningStart: vi.fn(),
  useMiningStatus: vi.fn(),
  useNetworkInfo: vi.fn(),
  useApiPoolsInfo: vi.fn(),
  useSystemInfo: vi.fn(),
  useSystemStatus: vi.fn(),
  useAccessToken: vi.fn(),
  useDefaultPasswordActive: vi.fn(),
  useDeviceTheme: vi.fn(),
  useFirmwareUpdateInstalling: vi.fn(),
  useFwUpdateStatus: vi.fn(),
  useHashboardSerials: vi.fn(),
  useIsMining: vi.fn(),
  useIsMiningDriverRunning: vi.fn(),
  useIsSleeping: vi.fn(),
  useIsWarmingUp: vi.fn(),
  useIsWebServerRunning: vi.fn(),
  useMinerErrors: vi.fn(),
  useOnboarded: vi.fn(),
  usePasswordSet: vi.fn(),
  usePoolsInfoStore: vi.fn(),
  useSetDeviceTheme: vi.fn(),
  useSetDismissedLoginModal: vi.fn(),
  useSetShowLoginModal: vi.fn(),
  useShowLoginModal: vi.fn(),
  useTheme: vi.fn(),
  useWakeDialog: vi.fn(),
  checkFirmwareUpdate: vi.fn(),
  reloadSystemInfo: vi.fn(),
  fetchMiningStatus: vi.fn(),
  startMining: vi.fn(),
}));

vi.mock("react-router-dom", () => ({
  useLocation: () => ({ pathname: "/", state: null }),
}));

vi.mock("./ErrorCallout", () => ({
  default: () => null,
}));

vi.mock("./WakeCallout", () => ({
  default: () => null,
}));

vi.mock("./WarmingUpCallout", () => ({
  default: () => null,
}));

vi.mock("@/protoOS/api/hooks/useErrors", () => ({
  useErrors: (params: unknown) => mocks.useErrors(params),
}));

vi.mock("@/protoOS/api/hooks/useFirmwareUpdate", () => ({
  useFirmwareUpdate: () => mocks.useFirmwareUpdate(),
}));

vi.mock("@/protoOS/api/hooks/useHardware", () => ({
  useHardware: (params: unknown) => mocks.useHardware(params),
}));

vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({
  useHashboardStatus: (params: unknown) => mocks.useHashboardStatus(params),
}));

vi.mock("@/protoOS/api/hooks/useMiningStart", () => ({
  useMiningStart: () => mocks.useMiningStart(),
}));

vi.mock("@/protoOS/api/hooks/useMiningStatus", () => ({
  useMiningStatus: (params: unknown) => mocks.useMiningStatus(params),
}));

vi.mock("@/protoOS/api/hooks/useNetworkInfo", () => ({
  useNetworkInfo: (params: unknown) => mocks.useNetworkInfo(params),
}));

vi.mock("@/protoOS/api/hooks/usePoolsInfo", () => ({
  usePoolsInfo: (params: unknown) => mocks.useApiPoolsInfo(params),
}));

vi.mock("@/protoOS/api/hooks/useSystemInfo", () => ({
  useSystemInfo: (params: unknown) => mocks.useSystemInfo(params),
}));

vi.mock("@/protoOS/api/hooks/useSystemStatus", () => ({
  useSystemStatus: () => mocks.useSystemStatus(),
}));

vi.mock("@/protoOS/components/AppLayout", () => ({
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/protoOS/components/ContentLayout/DefaultContentLayout", () => ({
  default: () => null,
}));

vi.mock("@/protoOS/components/NavigationMenu", () => ({
  navigationMenuTypes: { app: "app" },
}));

vi.mock("@/protoOS/components/NoPoolsCallout", () => ({
  default: () => null,
}));

vi.mock("@/protoOS/components/Power", () => ({
  WarnWakeDialog: () => null,
}));

vi.mock("@/protoOS/features/auth/components/LoginModal", () => ({
  default: () => null,
}));

vi.mock("@/protoOS/store", () => ({
  useAccessToken: () => mocks.useAccessToken(),
  useDefaultPasswordActive: () => mocks.useDefaultPasswordActive(),
  useDeviceTheme: () => mocks.useDeviceTheme(),
  useFirmwareUpdateInstalling: () => mocks.useFirmwareUpdateInstalling(),
  useFwUpdateStatus: () => mocks.useFwUpdateStatus(),
  useHashboardSerials: () => mocks.useHashboardSerials(),
  useIsMining: () => mocks.useIsMining(),
  useIsMiningDriverRunning: () => mocks.useIsMiningDriverRunning(),
  useIsSleeping: () => mocks.useIsSleeping(),
  useIsWarmingUp: () => mocks.useIsWarmingUp(),
  useIsWebServerRunning: () => mocks.useIsWebServerRunning(),
  useMinerErrors: () => mocks.useMinerErrors(),
  useOnboarded: () => mocks.useOnboarded(),
  usePasswordSet: () => mocks.usePasswordSet(),
  usePoolsInfo: () => mocks.usePoolsInfoStore(),
  useSetDeviceTheme: () => mocks.useSetDeviceTheme(),
  useSetDismissedLoginModal: () => mocks.useSetDismissedLoginModal(),
  useSetShowLoginModal: () => mocks.useSetShowLoginModal(),
  useShowLoginModal: () => mocks.useShowLoginModal(),
  useTheme: () => mocks.useTheme(),
  useWakeDialog: () => mocks.useWakeDialog(),
}));

vi.mock("@/shared/components/ErrorBoundary", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/shared/components/ProgressCircular", () => ({
  default: () => <div>loading</div>,
}));

vi.mock("@/shared/components/Setup", () => ({
  BootingUp: () => <div>booting</div>,
}));

vi.mock("@/shared/features/preferences", () => ({
  useApplyTheme: (...args: unknown[]) => args,
}));

vi.mock("@/shared/features/toaster", () => ({
  STATUSES: {
    error: "error",
    success: "success",
  },
  Toaster: () => null,
  pushToast: vi.fn(),
}));

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mocks.navigate,
}));

describe("App auth gating", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    mocks.useFirmwareUpdate.mockReturnValue({
      checkFirmwareUpdate: mocks.checkFirmwareUpdate.mockResolvedValue(undefined),
    });
    mocks.useMiningStart.mockReturnValue({ startMining: mocks.startMining });
    mocks.useMiningStatus.mockReturnValue({ data: {}, fetchData: mocks.fetchMiningStatus });
    mocks.useSystemInfo.mockReturnValue({ reload: mocks.reloadSystemInfo });
    mocks.useAccessToken.mockReturnValue({ hasAccess: true });
    mocks.useDefaultPasswordActive.mockReturnValue(false);
    mocks.useDeviceTheme.mockReturnValue("light");
    mocks.useFirmwareUpdateInstalling.mockReturnValue(false);
    mocks.useFwUpdateStatus.mockReturnValue(undefined);
    mocks.useHashboardSerials.mockReturnValue([]);
    mocks.useIsMining.mockReturnValue(false);
    mocks.useIsMiningDriverRunning.mockReturnValue(true);
    mocks.useIsSleeping.mockReturnValue(false);
    mocks.useIsWarmingUp.mockReturnValue(false);
    mocks.useIsWebServerRunning.mockReturnValue(true);
    mocks.useMinerErrors.mockReturnValue({ errors: [] });
    mocks.useOnboarded.mockReturnValue(true);
    mocks.usePasswordSet.mockReturnValue(true);
    mocks.usePoolsInfoStore.mockReturnValue(undefined);
    mocks.useSetDeviceTheme.mockReturnValue(vi.fn());
    mocks.useSetDismissedLoginModal.mockReturnValue(vi.fn());
    mocks.useSetShowLoginModal.mockReturnValue(vi.fn());
    mocks.useShowLoginModal.mockReturnValue(false);
    mocks.useTheme.mockReturnValue("light");
    mocks.useWakeDialog.mockReturnValue({ show: false, onClose: vi.fn(), onConfirm: vi.fn() });
  });

  it("keeps bootstrap reads disabled while access validation is still pending", () => {
    mocks.useAccessToken.mockReturnValue({ hasAccess: undefined });

    render(<App title="App" />);

    expect(mocks.useHardware).toHaveBeenCalledWith({ enabled: false });
    expect(mocks.useNetworkInfo).toHaveBeenCalledWith({ enabled: false, poll: false });
  });

  it("enables bootstrap reads after access validation succeeds", () => {
    const { rerender } = render(<App title="App" />);

    expect(mocks.useHardware).toHaveBeenCalledWith({ enabled: true });
    expect(mocks.useNetworkInfo).toHaveBeenCalledWith({ enabled: true, poll: false });

    mocks.useHardware.mockClear();
    mocks.useNetworkInfo.mockClear();
    mocks.useAccessToken.mockReturnValue({ hasAccess: undefined });
    rerender(<App title="App" />);

    expect(mocks.useHardware).toHaveBeenCalledWith({ enabled: false });
    expect(mocks.useNetworkInfo).toHaveBeenCalledWith({ enabled: false, poll: false });
  });

  it("keeps protected hooks disabled while defaultPasswordActive is still unresolved", () => {
    // On a reload with persisted tokens, hasAccess is true immediately but
    // defaultPasswordActive is undefined until /api/v1/system/status resolves.
    // Firing protected hooks in that window produces a burst of 403s on a
    // factory-password device before the redirect to the change-password flow.
    mocks.useAccessToken.mockReturnValue({ hasAccess: true });
    mocks.useDefaultPasswordActive.mockReturnValue(undefined);

    render(<App title="App" />);

    expect(mocks.useHardware).toHaveBeenCalledWith({ enabled: false });
    expect(mocks.useNetworkInfo).toHaveBeenCalledWith({ enabled: false, poll: false });
  });

  it("keeps protected hooks disabled when defaultPasswordActive is true", () => {
    mocks.useAccessToken.mockReturnValue({ hasAccess: true });
    mocks.useDefaultPasswordActive.mockReturnValue(true);

    render(<App title="App" />);

    expect(mocks.useHardware).toHaveBeenCalledWith({ enabled: false });
    expect(mocks.useNetworkInfo).toHaveBeenCalledWith({ enabled: false, poll: false });
  });
});
