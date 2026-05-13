/* eslint-disable react-refresh/only-export-components -- lazy() route components colocated with route config; not HMR-relevant */
import { ComponentType, lazy, ReactNode } from "react";
import { createBrowserRouter, Outlet, redirect, RouteObject } from "react-router-dom";

import {
  importDiagnosticView,
  importEfficiency,
  importHashboardTemperature,
  importHashrate,
  importLogs,
  importOnboarding,
  importOnboardingAuthentication,
  importOnboardingMiningPool,
  importOnboardingNetwork,
  importOnboardingVerify,
  importOnboardingWelcome,
  importPowerUsage,
  importSettingsAuthentication,
  importSettingsCooling,
  importSettingsGeneral,
  importSettingsHardware,
  importSettingsMiningPools,
  importTemperature,
} from "./routePrefetch";
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

// Route import factories live in `routePrefetch.ts` so consumers can
// import the tier arrays from there without a cycle through this file.

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
