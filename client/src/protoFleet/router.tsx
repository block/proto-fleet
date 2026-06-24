/* eslint-disable react-refresh/only-export-components -- lazy() route components colocated with route config; not HMR-relevant */
import { createElement, lazy, ReactNode } from "react";
import { createBrowserRouter, LoaderFunction, LoaderFunctionArgs, Navigate, Outlet, redirect } from "react-router-dom";

import App from "./components/App";
import SingleMinerWrapper from "./components/SingleMinerWrapper";
import type { PageBackground } from "./hooks/usePageBackground";
import {
  importActivityPage,
  importAuth,
  importBuildingPage,
  importDashboard,
  importEnergyPage,
  importFleetBuildingsPage,
  importFleetDown,
  importFleetInfraPage,
  importFleetLayout,
  importFleetSitesPage,
  importGroupOverviewPage,
  importGroupsPage,
  importMiners,
  importMinersPage,
  importOnboardingSettingsPage,
  importRackOverviewPage,
  importRacksPage,
  importSecurityPage,
  importServerLogsPage,
  importSettingsAlerts,
  importSettingsApiKeys,
  importSettingsAuth,
  importSettingsCurtailment,
  importSettingsFirmware,
  importSettingsGeneral,
  importSettingsLayout,
  importSettingsMiningPools,
  importSettingsRoles,
  importSettingsSchedules,
  importSettingsTeam,
  importSiteDetailPage,
  importUpdatePassword,
  importWelcomePage,
} from "./routePrefetch";
import { onboardingClient } from "@/protoFleet/api/clients";
import {
  minersRedirectLoader,
  racksRedirectLoader,
  sitesRedirectLoader,
} from "@/protoFleet/features/fleetManagement/redirectLoaders";
import {
  activeSiteFromSegment,
  appEntryPath,
  SiteScopeLayout,
  SiteScopeProvider,
} from "@/protoFleet/routing/siteScope";
import { sanitizeActiveSite } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
// eslint-disable-next-line no-restricted-imports -- Fleet shell embeds the protoOS single-miner experience
import { routerConfig as singleMinerRoutes } from "@/protoOS/router";

// Route import factories and prefetch tier arrays live in
// `routePrefetch.ts` so consumers can import the tiers without a cycle
// through this file. Auth metadata for the router lives in `routeAuth.ts`.

const Dashboard = lazy(importDashboard);
const Miners = lazy(importMiners);
const ActivityPage = lazy(importActivityPage);
const EnergyPage = lazy(importEnergyPage);
const ServerLogsPage = lazy(importServerLogsPage);
const GroupsPage = lazy(importGroupsPage);
const GroupOverviewPage = lazy(importGroupOverviewPage);
const RacksPage = lazy(importRacksPage);
const RackOverviewPage = lazy(importRackOverviewPage);
const Auth = lazy(importAuth);
const UpdatePassword = lazy(importUpdatePassword);
const WelcomePage = lazy(importWelcomePage);
const MinersPage = lazy(importMinersPage);
const SecurityPage = lazy(importSecurityPage);
const OnboardingSettingsPage = lazy(importOnboardingSettingsPage);
const SettingsLayout = lazy(importSettingsLayout);
const SettingsGeneral = lazy(importSettingsGeneral);
const SettingsAuth = lazy(importSettingsAuth);
const SettingsMiningPools = lazy(importSettingsMiningPools);
const SettingsTeam = lazy(importSettingsTeam);
const SettingsRoles = lazy(importSettingsRoles);
const SettingsFirmware = lazy(importSettingsFirmware);
const SettingsSchedules = lazy(importSettingsSchedules);
const SettingsCurtailment = lazy(importSettingsCurtailment);
const SettingsAlerts = lazy(importSettingsAlerts);
const SettingsApiKeys = lazy(importSettingsApiKeys);
const SiteDetailPage = lazy(importSiteDetailPage);
const BuildingPage = lazy(importBuildingPage);
const FleetLayout = lazy(importFleetLayout);
const FleetBuildingsPage = lazy(importFleetBuildingsPage);
const FleetSitesPage = lazy(importFleetSitesPage);
const FleetDown = lazy(importFleetDown);
const FleetInfraPage = lazy(importFleetInfraPage);

// Helper to check if an admin user has been created
const checkFleetInitStatus = async (): Promise<boolean> => {
  try {
    const response = await onboardingClient.getFleetInitStatus({});
    return response.status?.adminCreated ?? false;
  } catch (error) {
    console.error("Failed to fetch Fleet Init Status:", error);
    // Default to true (assume admin exists) to prevent disrupting existing users
    // If backend is temporarily unavailable, it's safer to show the login page
    // rather than incorrectly redirecting existing users to the onboarding flow
    return true;
  }
};

// Loader for /auth route - redirects to /welcome if no admin exists (first time setup)
const authLoader = async () => {
  const adminCreated = await checkFleetInitStatus();
  if (!adminCreated) {
    return redirect("/welcome");
  }
  return null;
};

// Loader for /welcome route - redirects to /auth if admin already exists
const welcomeLoader = async () => {
  const adminCreated = await checkFleetInitStatus();
  if (adminCreated) {
    return redirect("/auth");
  }
  return null;
};

const appEntryLoader = () => redirect(appEntryPath(sanitizeActiveSite(useFleetStore.getState().ui.activeSite)));

const scopedGroupDetailRedirectLoader = ({ params, request }: LoaderFunctionArgs) => {
  const activeSite = activeSiteFromSegment(params.siteScope);
  if (!activeSite) {
    return redirect("/");
  }

  useFleetStore.getState().ui.setActiveSite(activeSite);
  const url = new URL(request.url);
  const groupLabel = params.groupLabel ? encodeURIComponent(params.groupLabel) : "";
  return redirect(`/groups/${groupLabel}${url.search}`);
};

// Helper to create route objects with App wrapper
interface CreateRouteOptions {
  fullscreen?: boolean;
  loader?: LoaderFunction;
  bg?: PageBackground;
}

const createRoute = (path: string, children: ReactNode, options: CreateRouteOptions = {}) => ({
  path,
  element: <App fullscreen={options.fullscreen}>{children}</App>,
  ...(options.loader && { loader: options.loader }),
  ...(options.bg && { handle: { bg: options.bg } }),
});

const createFleetChildren = () => [
  { index: true, element: null },
  { path: "miners", element: <Miners /> },
  { path: "racks", element: <RacksPage /> },
  { path: "buildings", element: <FleetBuildingsPage /> },
  { path: "sites", element: <FleetSitesPage /> },
  { path: "infrastructure", element: <FleetInfraPage /> },
];

const fleetRouteElement = (
  <App>
    <FleetLayout />
  </App>
);

const createFleetRoute = (path: string) => ({
  path,
  element: fleetRouteElement,
  children: createFleetChildren(),
});

const createScopableRoutes = (absolute: boolean) => [
  ...(absolute ? [] : [{ index: true, element: <Navigate to="dashboard" replace /> }]),
  createRoute(absolute ? "/dashboard" : "dashboard", <Dashboard />, { bg: "surface-5" }),
  createFleetRoute(absolute ? "/fleet" : "fleet"),
  createRoute(absolute ? "/groups" : "groups", <GroupsPage />),
  createRoute(absolute ? "/energy" : "energy", <EnergyPage />),
  createRoute(absolute ? "/activity" : "activity", <ActivityPage />),
];

// Wrap protoOS routes with SingleMinerWrapper for /miners/:id/* paths
const wrappedMinerRoutes = singleMinerRoutes.map((route) => {
  if (!route.element) return route;

  const wrappedElement = createElement(SingleMinerWrapper, null, route.element);

  return {
    ...route,
    element: wrappedElement,
  };
});

/**
 * Router configuration - defines actual route tree with React elements
 */
const router = createBrowserRouter([
  {
    path: "/",
    loader: appEntryLoader,
  },

  {
    element: (
      <SiteScopeProvider value={{ kind: "all" }}>
        <Outlet />
      </SiteScopeProvider>
    ),
    children: createScopableRoutes(true),
  },
  {
    path: "/:siteScope",
    element: <SiteScopeLayout />,
    children: [...createScopableRoutes(false), { path: "groups/:groupLabel", loader: scopedGroupDetailRedirectLoader }],
  },

  { path: "/miners", loader: minersRedirectLoader },
  { path: "/racks", loader: racksRedirectLoader },

  createRoute("/racks/:rackId", <RackOverviewPage />, { bg: "surface-5" }),
  createRoute("/groups/:groupLabel", <GroupOverviewPage />, { bg: "surface-5" }),

  // /sites redirects into /fleet/sites.
  { path: "/sites", loader: sitesRedirectLoader },
  createRoute("/sites/:id", <SiteDetailPage />, { bg: "surface-5" }),
  createRoute("/buildings/:id", <BuildingPage />, { bg: "surface-5" }),

  // Single miner (fullscreen - protoOS routes handle layout)
  {
    ...createRoute("/miners/:id", <Outlet />, { fullscreen: true }),
    children: wrappedMinerRoutes,
  },

  // Settings routes
  {
    path: "/settings",
    loader: () => redirect("/settings/general"),
  },
  createRoute(
    "/settings/general",
    <SettingsLayout>
      <SettingsGeneral />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/security",
    <SettingsLayout>
      <SettingsAuth />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/mining-pools",
    <SettingsLayout>
      <SettingsMiningPools />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/team",
    <SettingsLayout>
      <SettingsTeam />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/roles",
    <SettingsLayout>
      <SettingsRoles />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/firmware",
    <SettingsLayout>
      <SettingsFirmware />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/schedules",
    <SettingsLayout>
      <SettingsSchedules />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/curtailment",
    <SettingsLayout>
      <SettingsCurtailment />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/alerts",
    <SettingsLayout>
      <SettingsAlerts />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/api-keys",
    <SettingsLayout>
      <SettingsApiKeys />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/server-logs",
    <SettingsLayout>
      <ServerLogsPage />
    </SettingsLayout>,
  ),
  // Auth routes (fullscreen)
  createRoute("/auth", <Auth />, { fullscreen: true, loader: authLoader }),
  createRoute("/update-password", <UpdatePassword />, { fullscreen: true }),
  createRoute("/welcome", <WelcomePage />, { fullscreen: true, loader: welcomeLoader }),

  // Onboarding routes
  createRoute("/onboarding/miners", <MinersPage />),
  createRoute("/onboarding/security", <SecurityPage />, { fullscreen: true }),
  createRoute("/onboarding/settings", <OnboardingSettingsPage />, { fullscreen: true }),

  // Error routes (fullscreen)
  createRoute("/fleet-down", <FleetDown />, { fullscreen: true }),
]);

export default router;
