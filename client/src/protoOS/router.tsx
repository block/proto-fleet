/* eslint-disable react-refresh/only-export-components -- lazy() route components colocated with route config; not HMR-relevant */
import { ComponentType, lazy, ReactNode } from "react";
import { createBrowserRouter, Navigate, Outlet, redirect, RouteObject, useSearchParams } from "react-router-dom";

import {
  importContainerHome,
  importContainerModules,
  importContainerPools,
  importContainerTanks,
  importDiagnosticView,
  importEfficiency,
  importHashboardTemperature,
  importHashrate,
  importLogs,
  importModuleHashboardOverlay,
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
import ListContentLayout from "@/protoOS/components/ContentLayout/ListContentLayout";
import SettingsContentLayout from "@/protoOS/components/ContentLayout/SettingsContentLayout";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import ContainerHomeLayout from "@/protoOS/features/containerHome/ContainerHomeLayout";
import type { HomeTab } from "@/protoOS/features/containerHome/types";
import KpiLayout from "@/protoOS/features/kpis/components/KpiLayout";
import { settingsRouteMetadata } from "@/protoOS/routeAuth";
import { useSystemInfo, useSystemInfoError } from "@/protoOS/store";
import ProgressCircular from "@/shared/components/ProgressCircular";

// Custom route type with requiresAuth property
export type CustomRouteObject = RouteObject & {
  requiresAuth?: boolean;
  children?: CustomRouteObject[];
};

// Route import factories live in `routePrefetch.ts` so consumers can
// import the tier arrays from there without a cycle through this file.

const Hashrate = lazy(importHashrate);
const ContainerHome = lazy(importContainerHome);
const Efficiency = lazy(importEfficiency);
const PowerUsage = lazy(importPowerUsage);
const Temperature = lazy(importTemperature);
const HashboardTemperature = lazy(importHashboardTemperature);
const DiagnosticView = lazy(importDiagnosticView);
const Logs = lazy(importLogs);
const ContainerTanks = lazy(importContainerTanks);
const ContainerModules = lazy(importContainerModules);
const ModuleHashboardOverlay = lazy(importModuleHashboardOverlay);
const ContainerPools = lazy(importContainerPools);
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

const RigSettingsRoute = ({
  children,
  allowUnknownDevice = false,
}: {
  children: ReactNode;
  allowUnknownDevice?: boolean;
}) => {
  const systemInfo = useSystemInfo();
  const systemInfoError = useSystemInfoError();
  const { minerRoot } = useMinerHosting();
  // Password recovery must remain reachable before a rig can return authenticated system info.
  if (!systemInfo && allowUnknownDevice) return children;
  if (!systemInfo && !systemInfoError) return <ProgressCircular indeterminate />;
  if (!systemInfo || systemInfo.identity.deviceType === "container") {
    return <Navigate to={`${minerRoot}/settings/general`} replace />;
  }
  return children;
};

const ContainerRoute = ({ children }: { children: ReactNode }) => {
  const systemInfo = useSystemInfo();
  const systemInfoError = useSystemInfoError();
  const { minerRoot } = useMinerHosting();

  if (systemInfo) {
    return systemInfo.identity.deviceType === "container" ? (
      children
    ) : (
      <Navigate to={`${minerRoot}/hashrate`} replace />
    );
  }

  if (systemInfoError) return <Navigate to={`${minerRoot}/hashrate`} replace />;

  return (
    <div className="flex h-full min-h-64 items-center justify-center">
      <ProgressCircular indeterminate />
    </div>
  );
};

const useSupportsContainerHome = () => {
  const systemInfo = useSystemInfo();
  const { adapter } = useMinerHosting();
  return systemInfo?.identity.deviceType === "container" && typeof adapter?.getContainerHistory === "function";
};

const HomeIndexRoute = () => {
  const systemInfo = useSystemInfo();
  const systemInfoError = useSystemInfoError();
  const supportsContainerHome = useSupportsContainerHome();
  // The System tab stays on the root path: the device serves only a fixed set of route prefixes.
  const tab = useSearchParams()[0].get("view") === "system" ? "system" : "overview";

  if (systemInfo) {
    return supportsContainerHome ? <ContainerHome tab={tab} /> : <Navigate to="hashrate" replace />;
  }

  if (systemInfoError) return <Navigate to="hashrate" replace />;

  return (
    <div className="flex h-full min-h-64 items-center justify-center">
      <ProgressCircular indeterminate />
    </div>
  );
};

const HomeContentLayout = () => {
  const systemInfo = useSystemInfo();
  const error = useSystemInfoError();
  const supportsContainerHome = useSupportsContainerHome();
  if (!systemInfo && !error)
    return (
      <div className="flex min-h-64 items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  return supportsContainerHome ? <ContainerHomeLayout /> : <KpiLayout />;
};

const HomeMetricRoute = ({ tab, children }: { tab: HomeTab; children: ReactNode }) => {
  const supportsContainerHome = useSupportsContainerHome();
  return supportsContainerHome ? <ContainerHome tab={tab} /> : children;
};

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

const kpiRoutes: CustomRouteObject[] = [
  {
    path: "hashrate",
    element: (
      <HomeMetricRoute tab="hashrate">
        <Hashrate />
      </HomeMetricRoute>
    ),
  },
  {
    path: "efficiency",
    element: (
      <HomeMetricRoute tab="efficiency">
        <Efficiency />
      </HomeMetricRoute>
    ),
  },
  {
    path: "power-usage",
    element: (
      <HomeMetricRoute tab="power">
        <PowerUsage />
      </HomeMetricRoute>
    ),
  },
  {
    path: "temperature",
    element: (
      <HomeMetricRoute tab="temperature">
        <Temperature />
      </HomeMetricRoute>
    ),
  },
];

export const routerConfig: CustomRouteObject[] = [
  {
    ...createRoute("", <Outlet />, {
      title: "Home",
      ContentLayout: HomeContentLayout,
    }),
    requiresAuth: false,
    children: [
      {
        index: true,
        element: <HomeIndexRoute />,
      },
      ...kpiRoutes,
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
  createRoute(
    "pools",
    <ContainerRoute>
      <ContainerPools />
    </ContainerRoute>,
    {
      title: "Pools",
      hideErrors: true,
      ContentLayout: FullScreenContentLayout,
    },
  ),
  createRoute("systems", <DiagnosticView />, {
    title: "Systems",
    hideErrors: true,
  }),
  createRoute("systems/:serial", <HashboardTemperature />, {
    title: "Systems",
    fullscreen: true,
  }),
  createRoute(
    "tanks/:tankId",
    <ContainerRoute>
      <ContainerTanks />
    </ContainerRoute>,
    {
      title: "Tanks",
    },
  ),
  createRoute(
    "tanks/:tankId/modules/:moduleId",
    <ContainerRoute>
      <ContainerTanks />
    </ContainerRoute>,
    { title: "Tanks" },
  ),
  createRoute(
    "tanks/:tankId/modules/:moduleId/:serial",
    <ContainerRoute>
      <HashboardTemperature />
    </ContainerRoute>,
    {
      title: "Hashboards",
      fullscreen: true,
    },
  ),
  createRoute(
    "tanks",
    <ContainerRoute>
      <ContainerTanks />
    </ContainerRoute>,
    {
      title: "Tanks",
    },
  ),
  {
    ...createRoute(
      "modules",
      <ContainerRoute>
        <ContainerModules />
      </ContainerRoute>,
      { title: "Modules", hideErrors: true, ContentLayout: ListContentLayout },
    ),
    children: [
      {
        path: ":tankId/:moduleId",
        element: <Outlet />,
        children: [{ path: ":serial", element: <ModuleHashboardOverlay /> }],
      },
    ],
  },
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
        element: (
          <RigSettingsRoute allowUnknownDevice>
            <SettingsAuthentication />
          </RigSettingsRoute>
        ),
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
        element: (
          <RigSettingsRoute>
            <SettingsCooling />
          </RigSettingsRoute>
        ),
        requiresAuth: settingsRouteMetadata.cooling.requiresAuth,
      },
    ],
  },
];

export const createRouter = () => createBrowserRouter(routerConfig);
