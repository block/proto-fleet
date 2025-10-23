import { ComponentType, ReactNode } from "react";
import {
  createBrowserRouter,
  Outlet,
  redirect,
  RouteObject,
} from "react-router-dom";

import { DiagnosticView } from "./features/diagnostic/components";
import App from "@/protoOS/components/App";
import FullScreenContentLayout from "@/protoOS/components/ContentLayout/FullScreenContentLayout";
import SettingsContentLayout from "@/protoOS/components/ContentLayout/SettingsContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";

// Custom route type with requiresAuth property
export type CustomRouteObject = RouteObject & {
  requiresAuth?: boolean;
  children?: CustomRouteObject[];
};
import {
  Efficiency,
  HashboardTemperature,
  Hashrate,
  KpiLayout,
  PowerUsage,
  Temperature,
} from "@/protoOS/features/kpis";
import {
  Authentication,
  MiningPool,
  Network,
  Onboarding,
  Verify,
  Welcome,
} from "@/protoOS/features/onboarding";
import {
  Authentication as AuthenticationSettings,
  Cooling,
  General,
  Hardware,
  MiningPools,
} from "@/protoOS/features/settings";
import Logs from "@/protoOS/pages/MinerLogs";

// Helper to create route objects with App wrapper
interface CreateRouteOptions {
  title: string;
  fullscreen?: boolean;
  hideErrors?: boolean;
  ContentLayout?: ComponentType<ContentLayoutProps>;
}

const createRoute = (
  path: string,
  children: ReactNode,
  options: CreateRouteOptions,
) => ({
  path,
  element: (
    <App
      title={options.title}
      fullscreen={options.fullscreen}
      hideErrors={options.hideErrors}
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
        path: "authentication",
        element: <AuthenticationSettings />,
      },
      {
        path: "general",
        element: <General />,
      },
      {
        path: "mining-pools",
        element: <MiningPools />,
        requiresAuth: true,
      },
      {
        path: "hardware",
        element: <Hardware />,
      },
      {
        path: "cooling",
        element: <Cooling />,
        requiresAuth: true,
      },
    ],
  },
];

export const createRouter = () => createBrowserRouter(routerConfig);
