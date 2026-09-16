import { type ComponentType, type ReactNode, Suspense } from "react";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import type { MinerAdapter } from "./api/adapters/types";
import type { ContentLayoutProps } from "./components/ContentLayout/types";
import { routerConfig } from "./router";

const mocks = vi.hoisted(() => ({
  minerRoot: "",
  systemInfo: undefined as { identity: { deviceType: "container" | "rig" } } | undefined,
  systemInfoError: undefined as string | undefined,
  adapter: {} as Pick<MinerAdapter, "getContainerHistory">,
}));

vi.mock("./routePrefetch", () => {
  const route = (label: string) => () => Promise.resolve({ default: () => <div>{label}</div> });
  return {
    importContainerHome: () => Promise.resolve({ default: ({ tab }: { tab: string }) => <div>container {tab}</div> }),
    importContainerModules: route("modules"),
    importContainerTanks: route("tanks"),
    importDiagnosticView: route("diagnostics"),
    importEfficiency: route("efficiency"),
    importHashboardTemperature: route("hashboard temperature"),
    importHashrate: route("hashrate"),
    importLogs: route("logs"),
    importOnboarding: route("onboarding"),
    importOnboardingAuthentication: route("onboarding authentication"),
    importOnboardingMiningPool: route("onboarding mining pool"),
    importOnboardingNetwork: route("onboarding network"),
    importOnboardingVerify: route("onboarding verify"),
    importOnboardingWelcome: route("onboarding welcome"),
    importPowerUsage: route("power usage"),
    importSettingsAuthentication: route("settings authentication"),
    importSettingsCooling: route("settings cooling"),
    importSettingsGeneral: route("settings general"),
    importSettingsHardware: route("settings hardware"),
    importSettingsMiningPools: route("settings mining pools"),
    importTemperature: route("temperature"),
  };
});

vi.mock("@/protoOS/components/App", () => ({
  default: ({
    children,
    ContentLayout,
  }: {
    children: ReactNode;
    ContentLayout?: ComponentType<ContentLayoutProps>;
  }) => (
    <Suspense fallback={<div>route loading</div>}>
      {ContentLayout ? <ContentLayout>{children}</ContentLayout> : children}
    </Suspense>
  ),
}));

vi.mock("@/protoOS/components/ContentLayout/FullScreenContentLayout", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/protoOS/components/ContentLayout/SettingsContentLayout", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ minerRoot: mocks.minerRoot, adapter: mocks.adapter }),
}));

vi.mock("@/protoOS/features/kpis/components/KpiLayout", () => ({
  default: () => (
    <div data-testid="kpi-layout">
      <Outlet />
    </div>
  ),
}));

vi.mock("@/protoOS/features/containerHome/ContainerHomeLayout", () => ({
  default: () => (
    <div data-testid="container-home-layout">
      <Outlet />
    </div>
  ),
}));

vi.mock("@/protoOS/store", () => ({
  useSystemInfo: () => mocks.systemInfo,
  useSystemInfoError: () => mocks.systemInfoError,
}));

vi.mock("@/shared/components/ProgressCircular", () => ({
  default: () => <div>loading</div>,
}));

describe("ProtoOS router", () => {
  beforeEach(() => {
    mocks.minerRoot = "";
    mocks.systemInfo = undefined;
    mocks.systemInfoError = undefined;
    mocks.adapter = { getContainerHistory: vi.fn() };
  });

  test.each([
    ["/", "hashrate"],
    ["/hashrate", "hashrate"],
    ["/efficiency", "efficiency"],
    ["/power-usage", "power usage"],
    ["/temperature", "temperature"],
  ])("keeps MDK v1 CUs on the KPI layout and page at %s", async (path, label) => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    // MDK v1 CUs identify as containers but omit the aggregate-history capability.
    mocks.adapter = {};
    const router = createMemoryRouter(routerConfig, { initialEntries: [path] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(screen.getByText(label)).toBeInTheDocument());
    expect(screen.getByTestId("kpi-layout")).toBeInTheDocument();
    expect(screen.queryByTestId("container-home-layout")).not.toBeInTheDocument();
    expect(router.state.location.pathname).toBe(path === "/" ? "/hashrate" : path);
  });

  test("renders Overview at the container index and keeps rig index redirects", async () => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    const container = createMemoryRouter(routerConfig, { initialEntries: ["/"] });
    const view = render(<RouterProvider router={container} />);
    await waitFor(() => expect(screen.getByText("container overview")).toBeInTheDocument());
    expect(screen.getByTestId("container-home-layout")).toBeInTheDocument();
    expect(container.state.location.pathname).toBe("/");
    view.unmount();
    mocks.systemInfo = { identity: { deviceType: "rig" } };
    const rig = createMemoryRouter(routerConfig, { initialEntries: ["/"] });
    render(<RouterProvider router={rig} />);
    await waitFor(() => expect(screen.getByText("hashrate")).toBeInTheDocument());
    expect(rig.state.location.pathname).toBe("/hashrate");
  });

  test("uses container Power while retaining container identity through transient errors", async () => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    mocks.systemInfoError = "Temporarily unavailable";
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/power-usage"] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(screen.getByText("container power")).toBeInTheDocument());
    expect(screen.getByTestId("container-home-layout")).toBeInTheDocument();
  });

  test.each(["cooling", "authentication"])("redirects a container away from rig settings/%s", async (path) => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    const router = createMemoryRouter(routerConfig, { initialEntries: [`/settings/${path}`] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(screen.getByText("settings general")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/settings/general");
  });

  test("preserves the Fleet miner root when redirecting container settings", async () => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    mocks.minerRoot = "/miners/ct1";
    const router = createMemoryRouter([{ path: "/miners/ct1", children: routerConfig }], {
      initialEntries: ["/miners/ct1/settings/cooling"],
    });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(screen.getByText("settings general")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/miners/ct1/settings/general");
  });

  test("keeps password recovery reachable before system info loads", async () => {
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/settings/authentication"] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(screen.getByText("settings authentication")).toBeInTheDocument());
  });

  test("keeps the home index pending while system info is still loading", () => {
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/"] });

    render(<RouterProvider router={router} />);

    expect(screen.getByText("loading")).toBeInTheDocument();
  });

  test("falls back to rig home instead of spinning forever when system info fails", async () => {
    mocks.systemInfoError = "Couldn't reach this miner";
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("hashrate")).toBeInTheDocument());
  });

  test("allows containers to enter tank routes", async () => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/tanks/1"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("tanks")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/tanks/1");
  });

  test("redirects rigs away from tank routes", async () => {
    mocks.systemInfo = { identity: { deviceType: "rig" } };
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/tanks/1"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("hashrate")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/hashrate");
  });

  test("allows containers to enter module modal routes", async () => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/tanks/1/modules/1"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("tanks")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/tanks/1/modules/1");
  });

  test("preserves container module routes when retained system info has a transient error", async () => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    mocks.systemInfoError = "Couldn't reach this miner";
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/tanks/1/modules/1"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("tanks")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/tanks/1/modules/1");
  });

  test("redirects rigs away from module modal routes", async () => {
    mocks.systemInfo = { identity: { deviceType: "rig" } };
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/tanks/1/modules/1"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("hashrate")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/hashrate");
  });
});
