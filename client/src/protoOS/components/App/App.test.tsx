import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  useLocation: vi.fn(),
  AuthenticatedShell: vi.fn(),
  LoginModal: vi.fn(),
  useMiningStart: vi.fn(),
  useMiningStatus: vi.fn(),
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
  reloadSystemInfo: vi.fn(),
  fetchMiningStatus: vi.fn(),
  startMining: vi.fn(),
}));

vi.mock(import("react-router-dom"), async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    useLocation: () => mocks.useLocation(),
  };
});

vi.mock("./AuthenticatedShell", () => ({
  default: (props: unknown) => {
    mocks.AuthenticatedShell(props);
    return null;
  },
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

vi.mock("@/protoOS/api/hooks/useMiningStart", () => ({
  useMiningStart: () => mocks.useMiningStart(),
}));

vi.mock("@/protoOS/api/hooks/useMiningStatus", () => ({
  useMiningStatus: (params: unknown) => mocks.useMiningStatus(params),
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
  default: (props: { onDismiss: () => void; onSuccess: () => void }) => {
    mocks.LoginModal(props);
    return null;
  },
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

    mocks.useLocation.mockReturnValue({ pathname: "/", state: null });
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

  it.each([
    { label: "access validation pending", hasAccess: undefined, defaultPasswordActive: false },
    { label: "defaultPasswordActive unresolved", hasAccess: true, defaultPasswordActive: undefined },
    { label: "defaultPasswordActive true", hasAccess: true, defaultPasswordActive: true },
  ])("does not mount AuthenticatedShell when $label", ({ hasAccess, defaultPasswordActive }) => {
    // defaultPasswordActive is undefined on reload until /api/v1/system/status
    // resolves; mounting the shell in that window would fire a 403 burst on a
    // factory-password device before the redirect to the change-password flow.
    mocks.useAccessToken.mockReturnValue({ hasAccess });
    mocks.useDefaultPasswordActive.mockReturnValue(defaultPasswordActive);

    render(<App title="App" />);

    expect(mocks.AuthenticatedShell).not.toHaveBeenCalled();
  });

  it("mounts AuthenticatedShell once access validation succeeds and unmounts it if access is lost", () => {
    const { rerender } = render(<App title="App" />);

    expect(mocks.AuthenticatedShell).toHaveBeenCalled();

    mocks.AuthenticatedShell.mockClear();
    mocks.useAccessToken.mockReturnValue({ hasAccess: undefined });
    rerender(<App title="App" />);

    expect(mocks.AuthenticatedShell).not.toHaveBeenCalled();
  });

  it("suppresses child render while default password is active off the change route", () => {
    // Without this gate, page-level hooks (useTelemetry etc.) that aren't
    // wired through canAccessProtectedApi would mount and fire 403s before
    // the redirect effect navigates to /onboarding/authentication.
    mocks.useAccessToken.mockReturnValue({ hasAccess: true });
    mocks.useDefaultPasswordActive.mockReturnValue(true);

    const children = <div data-testid="route-children">hashrate</div>;
    render(<App title="App">{children}</App>);

    expect(screen.queryByTestId("route-children")).not.toBeInTheDocument();
  });

  it.each([
    { label: "hasAccess pending", hasAccess: undefined, defaultPasswordActive: false },
    { label: "defaultPasswordActive pending", hasAccess: true, defaultPasswordActive: undefined },
  ])("suppresses child render on auth-required routes when $label", ({ hasAccess, defaultPasswordActive }) => {
    // useSystemStatus stages three store writes (onboarded → passwordSet →
    // defaultPasswordActive), so route-level hooks must stay unmounted until
    // canAccessProtectedApi resolves — otherwise they fire 401/403s in the gap.
    mocks.useLocation.mockReturnValue({ pathname: "/hashrate", state: null });
    mocks.useAccessToken.mockReturnValue({ hasAccess });
    mocks.useDefaultPasswordActive.mockReturnValue(defaultPasswordActive);

    const children = <div data-testid="route-children">hashrate</div>;
    render(<App title="App">{children}</App>);

    expect(screen.queryByTestId("route-children")).not.toBeInTheDocument();
  });

  it("renders children on onboarding routes even without resolved access", () => {
    // Onboarding runs before credentials exist, so the auth gate must not
    // block it — otherwise first-boot users are stuck on a blank screen.
    mocks.useLocation.mockReturnValue({ pathname: "/onboarding/welcome", state: null });
    mocks.useAccessToken.mockReturnValue({ hasAccess: undefined });
    mocks.useOnboarded.mockReturnValue(false);
    mocks.usePasswordSet.mockReturnValue(false);

    const children = <div data-testid="route-children">welcome</div>;
    render(<App title="App">{children}</App>);

    expect(screen.getByTestId("route-children")).toBeInTheDocument();
  });

  it.each(["/", "/hashrate", "/settings/general", "/settings/hardware", "/settings/mining-pools"])(
    "dismissing the login modal from %s routes to the public authentication page",
    (pathname) => {
      // Every auth-required route is render-gated when canAccessProtectedApi
      // is false, so dismissing must land on a public route or the user is
      // trapped behind the modal with no reachable page to return to.
      mocks.useLocation.mockReturnValue({ pathname, state: null });
      mocks.useAccessToken.mockReturnValue({ hasAccess: false });
      mocks.useShowLoginModal.mockReturnValue(true);

      render(<App title="App" />);

      const calls = mocks.LoginModal.mock.calls;
      const [{ onDismiss }] = calls[calls.length - 1] as [{ onDismiss: () => void }];
      onDismiss();

      expect(mocks.navigate).toHaveBeenCalledWith("/settings/authentication");
    },
  );

  it("does not navigate away when dismissing the login modal from /settings/authentication", () => {
    // The password-reset page is public and is the dismiss target itself —
    // a redirect would be either a no-op or an infinite loop.
    mocks.useLocation.mockReturnValue({ pathname: "/settings/authentication", state: null });
    mocks.useAccessToken.mockReturnValue({ hasAccess: false });
    mocks.useShowLoginModal.mockReturnValue(true);

    render(<App title="App" />);

    const calls = mocks.LoginModal.mock.calls;
    const [{ onDismiss }] = calls[calls.length - 1] as [{ onDismiss: () => void }];
    onDismiss();

    expect(mocks.navigate).not.toHaveBeenCalled();
  });
});
