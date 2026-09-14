import { type ReactNode, Suspense } from "react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { routerConfig } from "./router";

const mocks = vi.hoisted(() => ({
  minerRoot: "",
  systemInfo: undefined as { identity: { deviceType: "container" | "rig" } } | undefined,
  systemInfoError: undefined as string | undefined,
}));

vi.mock("./routePrefetch", () => {
  const route = (label: string) => () => Promise.resolve({ default: () => <div>{label}</div> });
  return {
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
  default: ({ children }: { children: ReactNode }) => (
    <Suspense fallback={<div>route loading</div>}>{children}</Suspense>
  ),
}));

vi.mock("@/protoOS/components/ContentLayout/FullScreenContentLayout", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/protoOS/components/ContentLayout/SettingsContentLayout", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ minerRoot: mocks.minerRoot }),
}));

vi.mock("@/protoOS/features/kpis/components/KpiLayout", () => ({
  default: ({ children }: { children?: ReactNode }) => <>{children}</>,
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

  test("allows containers to enter module KPI routes", async () => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/tanks/1/modules/1"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("hashrate")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/tanks/1/modules/1");
  });

  test("preserves container module routes when retained system info has a transient error", async () => {
    mocks.systemInfo = { identity: { deviceType: "container" } };
    mocks.systemInfoError = "Couldn't reach this miner";
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/tanks/1/modules/1"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("hashrate")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/tanks/1/modules/1");
  });

  test("redirects rigs away from module KPI routes", async () => {
    mocks.systemInfo = { identity: { deviceType: "rig" } };
    const router = createMemoryRouter(routerConfig, { initialEntries: ["/tanks/1/modules/1"] });

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("hashrate")).toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/hashrate");
  });
});
