/* eslint-disable react-refresh/only-export-components -- lazy() route components colocated with route config; not HMR-relevant */
import { ComponentType, lazy, ReactNode } from "react";
import { createBrowserRouter, Outlet, redirect, RouteObject } from "react-router-dom";

import App from "@/protoOS/components/App";
import FullScreenContentLayout from "@/protoOS/components/ContentLayout/FullScreenContentLayout";
import SettingsContentLayout from "@/protoOS/components/ContentLayout/SettingsContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import KpiLayout from "@/protoOS/features/kpis/components/KpiLayout";
import { settingsRouteMetadata } from "@/protoOS/routeAuth";

// Custom route type with requiresAuth property
export type CustomRouteObject = RouteObject & {
  requiresAuth?: boolean;
  children?: CustomRouteObject[];
};

// Route components are lazy so each pulls a separate chunk and protoFleet (which
// embeds these routes via singleMinerRoutes) stays slim until the user enters
// /miners/:id/*.
const Hashrate = lazy(() => import("@/protoOS/features/kpis/components/Hashrate"));
const Efficiency = lazy(() => import("@/protoOS/features/kpis/components/Efficiency"));
const PowerUsage = lazy(() => import("@/protoOS/features/kpis/components/PowerUsage"));
const Temperature = lazy(() => import("@/protoOS/features/kpis/components/Temperature"));
const HashboardTemperature = lazy(() => import("@/protoOS/features/diagnostic/components/HashboardTemperature"));
const DiagnosticView = lazy(() => import("@/protoOS/features/diagnostic/components/DiagnosticView/DiagnosticView"));
const Logs = lazy(() => import("@/protoOS/pages/MinerLogs"));
const Onboarding = lazy(() => import("@/protoOS/features/onboarding/components/Onboarding"));
const OnboardingWelcome = lazy(() => import("@/protoOS/features/onboarding/components/Welcome"));
const OnboardingVerify = lazy(() => import("@/protoOS/features/onboarding/components/Verify"));
const OnboardingNetwork = lazy(() => import("@/protoOS/features/onboarding/components/Network"));
const OnboardingAuthentication = lazy(() => import("@/protoOS/features/onboarding/components/Authentication"));
const OnboardingMiningPool = lazy(() => import("@/protoOS/features/onboarding/components/MiningPool"));
const SettingsAuthentication = lazy(() => import("@/protoOS/features/settings/components/Authentication"));
const SettingsGeneral = lazy(() => import("@/protoOS/features/settings/components/General"));
const SettingsMiningPools = lazy(() => import("@/protoOS/features/settings/components/MiningPools"));
const SettingsHardware = lazy(() => import("@/protoOS/features/settings/components/Hardware"));
const SettingsCooling = lazy(() => import("@/protoOS/features/settings/components/Cooling"));

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
