/* eslint-disable react-refresh/only-export-components -- lazy() route components colocated with route config; not HMR-relevant */
import { lazy, ReactNode } from "react";
import { createBrowserRouter, LoaderFunction, LoaderFunctionArgs, Navigate, Outlet, redirect } from "react-router-dom";

import App from "./components/App";
import SingleMinerWrapper from "./components/SingleMinerWrapper";
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
  importSettingsAgents,
  importSettingsAlerts,
  importSettingsAuth,
  importSettingsCurtailment,
  importSettingsFirmware,
  importSettingsIntegrations,
  importSettingsLayout,
  importSettingsMiningPools,
  importSettingsNetwork,
  importSettingsNodes,
  importSettingsPreferences,
  importSettingsSchedules,
  importSettingsTeam,
  importSiteDetailPage,
  importUpdatePassword,
  importWelcomePage,
} from "./routePrefetch";
import { onboardingClient, sitesClient } from "@/protoFleet/api/clients";
import { getSettingsLandingPath } from "@/protoFleet/config/navItems";
import {
  minersRedirectLoader,
  racksRedirectLoader,
  sitesRedirectLoader,
} from "@/protoFleet/features/fleetManagement/redirectLoaders";
import { hideShellHeaderRouteHandle } from "@/protoFleet/routing/routeHandle";
import {
  activeSiteFromSegment,
  appEntryPath,
  SiteScopeLayout,
  SiteScopeProvider,
} from "@/protoFleet/routing/siteScope";
import { type ActiveSite, sanitizeActiveSite } from "@/protoFleet/store/types/activeSite";
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
const SettingsNetwork = lazy(importSettingsNetwork);
const SettingsPreferences = lazy(importSettingsPreferences);
const SettingsAuth = lazy(importSettingsAuth);
const SettingsMiningPools = lazy(importSettingsMiningPools);
const SettingsTeam = lazy(importSettingsTeam);
const SettingsFirmware = lazy(importSettingsFirmware);
const SettingsNodes = lazy(importSettingsNodes);
const SettingsSchedules = lazy(importSettingsSchedules);
const SettingsCurtailment = lazy(importSettingsCurtailment);
const SettingsAlerts = lazy(importSettingsAlerts);
const SettingsAgents = lazy(importSettingsAgents);
const SettingsIntegrations = lazy(importSettingsIntegrations);
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

const settingsRedirectLoader = () => redirect(getSettingsLandingPath(useFleetStore.getState().auth.permissions));

// Group detail is canonical/unscoped, so a scoped URL like
// /north/groups/team-a redirects to /groups/team-a while preserving the
// operator's site selection. Site slugs only resolve to an id via the sites
// list, which a synchronous loader doesn't have — so resolve the slug over
// the wire (unassigned needs no lookup) and set the store scope before
// redirecting. An unresolvable segment still lands on the group page (the
// page is unscoped) rather than bouncing the user to "/".
const resolveScopeSegment = async (segment: string | undefined, signal: AbortSignal): Promise<ActiveSite | null> => {
  // Covers "unassigned"; site slugs return null here without a slug map.
  const local = activeSiteFromSegment(segment);
  if (local) return local;
  if (!segment) return null;
  try {
    const response = await sitesClient.resolveSiteBySlug({ slug: segment }, { signal });
    const id = (response.site?.id ?? 0n).toString();
    if (response.site?.slug && id !== "0") {
      return { kind: "site", id, slug: response.site.slug };
    }
  } catch {
    // Unknown or unreachable slug (or an aborted request) — fall through.
  }
  return null;
};

const scopedGroupDetailRedirectLoader = async ({ params, request }: LoaderFunctionArgs) => {
  const scope = await resolveScopeSegment(params.siteScope, request.signal);
  // The slug lookup is async, so a superseded navigation may have aborted this
  // loader while it was in flight. Skip the store write + redirect in that case
  // so a stale route can't leave the picker scoped to the wrong site.
  if (request.signal.aborted) {
    return null;
  }
  if (scope) {
    useFleetStore.getState().ui.setActiveSite(scope);
  }
  const url = new URL(request.url);
  const groupLabel = params.groupLabel ? encodeURIComponent(params.groupLabel) : "";
  return redirect(`/groups/${groupLabel}${url.search}`);
};

// Helper to create route objects with App wrapper
interface CreateRouteOptions {
  fullscreen?: boolean;
  hideShellHeader?: boolean;
  loader?: LoaderFunction;
}

const createRoute = (path: string, children: ReactNode, options: CreateRouteOptions = {}) => ({
  path,
  element: <App fullscreen={options.fullscreen}>{children}</App>,
  ...(options.hideShellHeader && { handle: hideShellHeaderRouteHandle }),
  ...(options.loader && { loader: options.loader }),
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
  createRoute(absolute ? "/dashboard" : "dashboard", <Dashboard />),
  createFleetRoute(absolute ? "/fleet" : "fleet"),
  createRoute(absolute ? "/groups" : "groups", <GroupsPage />),
  createRoute(absolute ? "/energy" : "energy", <EnergyPage />),
  createRoute(absolute ? "/activity" : "activity", <ActivityPage />),
];

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

  createRoute("/racks/:rackId", <RackOverviewPage />, { hideShellHeader: true }),
  createRoute("/groups/:groupLabel", <GroupOverviewPage />, { hideShellHeader: true }),

  // /sites redirects into /fleet/sites.
  { path: "/sites", loader: sitesRedirectLoader },
  createRoute("/sites/:id", <SiteDetailPage />, { hideShellHeader: true }),
  createRoute("/buildings/:id", <BuildingPage />, { hideShellHeader: true }),

  // Single miner (fullscreen - protoOS routes handle layout). SingleMinerWrapper
  // wraps the parent Outlet so it stays mounted across tab navigations — the
  // protoOS tabs redirect via loaders, which would otherwise remount it (and
  // replay its open animation) on every tab.
  {
    ...createRoute(
      "/miners/:id",
      <SingleMinerWrapper>
        <Outlet />
      </SingleMinerWrapper>,
      { fullscreen: true },
    ),
    children: singleMinerRoutes,
  },

  // Settings routes
  {
    path: "/settings",
    loader: settingsRedirectLoader,
  },
  {
    path: "/settings/general",
    loader: settingsRedirectLoader,
  },
  createRoute(
    "/settings/network",
    <SettingsLayout>
      <SettingsNetwork />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/preferences",
    <SettingsLayout>
      <SettingsPreferences />
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
  {
    path: "/settings/roles",
    loader: () => redirect("/settings/team?tab=roles"),
  },
  createRoute(
    "/settings/firmware",
    <SettingsLayout>
      <SettingsFirmware />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/nodes",
    <SettingsLayout>
      <SettingsNodes />
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
  {
    path: "/settings/api-keys",
    loader: () => redirect("/settings/integrations"),
  },
  createRoute(
    "/settings/agents",
    <SettingsLayout>
      <SettingsAgents />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/integrations",
    <SettingsLayout>
      <SettingsIntegrations />
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
