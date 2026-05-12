/* eslint-disable react-refresh/only-export-components -- lazy() route components colocated with route config; not HMR-relevant */
import { ComponentType, lazy, ReactNode } from "react";
import { createBrowserRouter, Outlet, redirect, RouteObject } from "react-router-dom";

import App from "@/protoOS/components/App";
import FullScreenContentLayout from "@/protoOS/components/ContentLayout/FullScreenContentLayout";
import SettingsContentLayout from "@/protoOS/components/ContentLayout/SettingsContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import KpiLayout from "@/protoOS/features/kpis/components/KpiLayout";
import { settingsRouteMetadata } from "@/protoOS/routeAuth";
import type { RouteImporter } from "@/shared/utils/prefetchRoutes";

// Custom route type with requiresAuth property
export type CustomRouteObject = RouteObject & {
  requiresAuth?: boolean;
  children?: CustomRouteObject[];
};

// Route components are lazy so each ships in its own chunk and
// protoFleet (which embeds these via singleMinerRoutes) stays slim
// until the user enters /miners/:id/*. Factories are hoisted so
// prefetchRoutes() can call them at idle.
//
// To add a route: define the factory const, wrap it with lazy(), and
// add the factory to the relevant tier export (globalRoutePrefetch or
// settingsRoutePrefetch — singleMinerRoutePrefetch composes both).
// Step 3 isn't lint-enforced — a missed entry leaves the chunk
// un-warmed without breaking the build.
const importHashrate = () => import("@/protoOS/features/kpis/components/Hashrate");
const importEfficiency = () => import("@/protoOS/features/kpis/components/Efficiency");
const importPowerUsage = () => import("@/protoOS/features/kpis/components/PowerUsage");
const importTemperature = () => import("@/protoOS/features/kpis/components/Temperature");
const importHashboardTemperature = () => import("@/protoOS/features/diagnostic/components/HashboardTemperature");
const importDiagnosticView = () => import("@/protoOS/features/diagnostic/components/DiagnosticView/DiagnosticView");
const importLogs = () => import("@/protoOS/pages/MinerLogs");
const importOnboarding = () => import("@/protoOS/features/onboarding/components/Onboarding");
const importOnboardingWelcome = () => import("@/protoOS/features/onboarding/components/Welcome");
const importOnboardingVerify = () => import("@/protoOS/features/onboarding/components/Verify");
const importOnboardingNetwork = () => import("@/protoOS/features/onboarding/components/Network");
const importOnboardingAuthentication = () => import("@/protoOS/features/onboarding/components/Authentication");
const importOnboardingMiningPool = () => import("@/protoOS/features/onboarding/components/MiningPool");
const importSettingsAuthentication = () => import("@/protoOS/features/settings/components/Authentication");
const importSettingsGeneral = () => import("@/protoOS/features/settings/components/General");
const importSettingsMiningPools = () => import("@/protoOS/features/settings/components/MiningPools");
const importSettingsHardware = () => import("@/protoOS/features/settings/components/Hardware");
const importSettingsCooling = () => import("@/protoOS/features/settings/components/Cooling");

const Hashrate = lazy(importHashrate);
const Efficiency = lazy(importEfficiency);
const PowerUsage = lazy(importPowerUsage);
const Temperature = lazy(importTemperature);
const HashboardTemperature = lazy(importHashboardTemperature);
const DiagnosticView = lazy(importDiagnosticView);
const Logs = lazy(importLogs);
const Onboarding = lazy(importOnboarding);
const OnboardingWelcome = lazy(importOnboardingWelcome);
const OnboardingVerify = lazy(importOnboardingVerify);
const OnboardingNetwork = lazy(importOnboardingNetwork);
const OnboardingAuthentication = lazy(importOnboardingAuthentication);
const OnboardingMiningPool = lazy(importOnboardingMiningPool);
const SettingsAuthentication = lazy(importSettingsAuthentication);
const SettingsGeneral = lazy(importSettingsGeneral);
const SettingsMiningPools = lazy(importSettingsMiningPools);
const SettingsHardware = lazy(importSettingsHardware);
const SettingsCooling = lazy(importSettingsCooling);

// Top-level destinations reachable from the protoOS sidebar plus the
// KPI tab-strip siblings of the default landing route. App.tsx
// triggers this at idle.
export const globalRoutePrefetch: readonly RouteImporter[] = [
  importHashrate,
  importEfficiency,
  importPowerUsage,
  importTemperature,
  importDiagnosticView,
  importLogs,
  importSettingsGeneral,
];

// Settings sub-routes; SettingsContentLayout triggers this on mount so the
// tab strip is warm by the time the user clicks across.
export const settingsRoutePrefetch: readonly RouteImporter[] = [
  importSettingsAuthentication,
  importSettingsMiningPools,
  importSettingsHardware,
  importSettingsCooling,
];

// Section tier for protoFleet — embeds this router under /miners/:id/*.
// SingleMinerWrapper triggers this on mount so KPI tabs, Logs,
// Diagnostics, and per-miner Settings are warm. Composes global +
// settings tiers so new entries flow through automatically.
export const singleMinerRoutePrefetch: readonly RouteImporter[] = [
  ...globalRoutePrefetch,
  importHashboardTemperature,
  ...settingsRoutePrefetch,
];

// Helper to create route objects with App wrapper
interface CreateRouteOptions {
  title: string;
  fullscreen?: boolean;
  hideErrors?: boolean;
  calloutTopSpacing?: boolean;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const createRoute = (path: string, children: ReactNode, options: CreateRouteOptions) => ({
  path,
  element: (
    <App
      title={options.title}
      fullscreen={options.fullscreen}
      hideErrors={options.hideErrors}
      calloutTopSpacing={options.calloutTopSpacing}
      ContentLayout={options.ContentLayout}
    >
      {children}
    </App>
  ),
});

export const routerConfig: CustomRouteObject[] = [
  {
    ...createRoute("", <Outlet />, {
      title: "Home",
      ContentLayout: KpiLayout,
    }),
    requiresAuth: false,
    children: [
      {
        index: true,
        loader: () => redirect("hashrate"),
      },
      {
        path: "hashrate",
        element: <Hashrate />,
      },
      {
        path: "efficiency",
        element: <Efficiency />,
      },
      {
        path: "power-usage",
        element: <PowerUsage />,
      },
      {
        path: "temperature",
        element: <Temperature />,
      },
    ],
  },
  createRoute("temperature/:serial", <HashboardTemperature />, {
    title: "Temperature",
    fullscreen: true,
  }),
  createRoute("logs", <Logs />, {
    title: "Logs",
    hideErrors: true,
    calloutTopSpacing: true,
    ContentLayout: FullScreenContentLayout,
  }),
  createRoute("diagnostics", <DiagnosticView />, {
    title: "Diagnostics",
    hideErrors: true,
  }),
  createRoute("diagnostics/:serial", <HashboardTemperature />, {
    title: "Diagnostics",
    fullscreen: true,
  }),
  // Note: Onboarding renders AppLayout directly in fullscreen mode
  createRoute("onboarding", <Onboarding />, {
    title: "Onboarding",
    fullscreen: true,
  }),
  createRoute("onboarding/welcome", <OnboardingWelcome />, {
    title: "Welcome",
    fullscreen: true,
  }),
  createRoute("onboarding/verify", <OnboardingVerify />, {
    title: "Verify",
    fullscreen: true,
  }),
  createRoute("onboarding/network", <OnboardingNetwork />, {
    title: "Network",
    fullscreen: true,
  }),
  createRoute("onboarding/authentication", <OnboardingAuthentication />, {
    title: "Authentication",
    fullscreen: true,
  }),
  createRoute("onboarding/mining-pool", <OnboardingMiningPool />, {
    title: "Mining Pool",
    fullscreen: true,
  }),
  {
    ...createRoute("settings", <Outlet />, {
      title: "Settings",
      ContentLayout: SettingsContentLayout,
    }),
    children: [
      {
        index: true,
        loader: () => redirect("general"),
      },
      {
        path: settingsRouteMetadata.authentication.path,
        element: <SettingsAuthentication />,
      },
      {
        path: settingsRouteMetadata.general.path,
        element: <SettingsGeneral />,
      },
      {
        path: settingsRouteMetadata.miningPools.path,
        element: <SettingsMiningPools />,
        requiresAuth: settingsRouteMetadata.miningPools.requiresAuth,
      },
      {
        path: settingsRouteMetadata.hardware.path,
        element: <SettingsHardware />,
      },
      {
        path: settingsRouteMetadata.cooling.path,
        element: <SettingsCooling />,
        requiresAuth: settingsRouteMetadata.cooling.requiresAuth,
      },
    ],
  },
];

export const createRouter = () => createBrowserRouter(routerConfig);
