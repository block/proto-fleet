import { ComponentType, ReactNode } from "react";
import { createBrowserRouter, Outlet, redirect, RouteObject } from "react-router-dom";

import { DiagnosticView } from "./features/diagnostic/components";
import HashboardTemperature from "./features/diagnostic/components/HashboardTemperature";
import App from "@/protoOS/components/App";
import FullScreenContentLayout from "@/protoOS/components/ContentLayout/FullScreenContentLayout";
import SettingsContentLayout from "@/protoOS/components/ContentLayout/SettingsContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

// Custom route type with requiresAuth property
export type CustomRouteObject = RouteObject & {
  requiresAuth?: boolean;
  children?: CustomRouteObject[];
};
import { Efficiency, Hashrate, KpiLayout, PowerUsage, Temperature } from "@/protoOS/features/kpis";
import { Authentication, MiningPool, Network, Onboarding, Verify, Welcome } from "@/protoOS/features/onboarding";
import {
  Authentication as AuthenticationSettings,
  Cooling,
  General,
  Hardware,
  MiningPools,
} from "@/protoOS/features/settings";
import Logs from "@/protoOS/pages/MinerLogs";
import { settingsRouteMetadata } from "@/protoOS/routeAuth";

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
  createRoute("onboarding/welcome", <Welcome />, {
    title: "Welcome",
    fullscreen: true,
  }),
  createRoute("onboarding/verify", <Verify />, {
    title: "Verify",
    fullscreen: true,
  }),
  createRoute("onboarding/network", <Network />, {
    title: "Network",
    fullscreen: true,
  }),
  createRoute("onboarding/authentication", <Authentication />, {
    title: "Authentication",
    fullscreen: true,
  }),
  createRoute("onboarding/mining-pool", <MiningPool />, {
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
        element: <AuthenticationSettings />,
      },
      {
        path: settingsRouteMetadata.general.path,
        element: <General />,
      },
      {
        path: settingsRouteMetadata.miningPools.path,
        element: <MiningPools />,
        requiresAuth: settingsRouteMetadata.miningPools.requiresAuth,
      },
      {
        path: settingsRouteMetadata.hardware.path,
        element: <Hardware />,
      },
      {
        path: settingsRouteMetadata.cooling.path,
        element: <Cooling />,
        requiresAuth: settingsRouteMetadata.cooling.requiresAuth,
      },
    ],
  },
];

export const createRouter = () => createBrowserRouter(routerConfig);
