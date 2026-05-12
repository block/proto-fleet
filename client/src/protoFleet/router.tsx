/* eslint-disable react-refresh/only-export-components -- lazy() route components colocated with route config; not HMR-relevant */
import { createElement, lazy, ReactNode } from "react";
import { createBrowserRouter, LoaderFunction, Outlet, redirect } from "react-router-dom";

import App from "./components/App";
import SingleMinerWrapper from "./components/SingleMinerWrapper";
import type { PageBackground } from "./hooks/usePageBackground";
import { onboardingClient } from "@/protoFleet/api/clients";
// eslint-disable-next-line no-restricted-imports -- Fleet shell embeds the protoOS single-miner experience
import { singleMinerRoutePrefetch, routerConfig as singleMinerRoutes } from "@/protoOS/router";
import type { RouteImporter } from "@/shared/utils/prefetchRoutes";

// Re-exported so SingleMinerWrapper imports from this file rather
// than crossing the protoOS boundary directly — consolidates the
// cross-app coupling to one place.
export { singleMinerRoutePrefetch };

// Route components are lazy so each ships in its own chunk; factories
// are hoisted so prefetchRoutes() can call them at idle.
//
// To add a route: define the factory const, wrap it with lazy(), and
// add the factory to the relevant tier export (globalRoutePrefetch or
// settingsRoutePrefetch). Step 3 isn't lint-enforced — a missed entry
// leaves the chunk un-warmed without breaking the build.
const importDashboard = () => import("@/protoFleet/features/dashboard/pages/Dashboard");
const importMiners = () => import("./features/fleetManagement/components/Fleet");
const importActivityPage = () => import("@/protoFleet/features/activity/pages/ActivityPage");
const importGroupsPage = () => import("@/protoFleet/features/groupManagement/pages/GroupsPage");
const importGroupOverviewPage = () => import("@/protoFleet/features/groupManagement/pages/GroupOverviewPage");
const importRacksPage = () => import("@/protoFleet/features/rackManagement/pages/RacksPage");
const importRackOverviewPage = () => import("@/protoFleet/features/rackManagement/pages/RackOverviewPage");
const importAuth = () => import("@/protoFleet/features/auth/pages/Auth");
const importUpdatePassword = () => import("@/protoFleet/features/auth/pages/UpdatePassword");
const importWelcomePage = () => import("@/protoFleet/features/onboarding/components/Welcome");
const importMinersPage = () => import("@/protoFleet/features/onboarding/components/Miners");
const importSecurityPage = () => import("@/protoFleet/features/onboarding/components/Security");
const importOnboardingSettingsPage = () => import("@/protoFleet/features/onboarding/components/Settings");
const importSettingsLayout = () => import("@/protoFleet/features/settings/components/SettingsLayout");
const importSettingsGeneral = () => import("@/protoFleet/features/settings/components/General");
const importSettingsAuth = () => import("@/protoFleet/features/settings/components/Auth");
const importSettingsMiningPools = () => import("@/protoFleet/features/settings/components/MiningPools");
const importSettingsTeam = () => import("@/protoFleet/features/settings/components/Team");
const importSettingsFirmware = () => import("@/protoFleet/features/settings/components/Firmware");
const importSettingsSchedules = () => import("@/protoFleet/features/settings/components/Schedules/SchedulesPage");
const importSettingsApiKeys = () => import("@/protoFleet/features/settings/components/ApiKeys");
const importFleetDown = () => import("@/protoFleet/components/FleetDown/FleetDown");

const Dashboard = lazy(importDashboard);
const Miners = lazy(importMiners);
const ActivityPage = lazy(importActivityPage);
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
const SettingsFirmware = lazy(importSettingsFirmware);
const SettingsSchedules = lazy(importSettingsSchedules);
const SettingsApiKeys = lazy(importSettingsApiKeys);
const FleetDown = lazy(importFleetDown);

// Sidebar destinations + the default settings sub-route. App.tsx
// triggers this at idle so the first nav click has no Suspense flash.
export const globalRoutePrefetch: readonly RouteImporter[] = [
  importDashboard,
  importMiners,
  importGroupsPage,
  importRacksPage,
  importActivityPage,
  importSettingsLayout,
  importSettingsGeneral,
];

// Settings sub-routes; SettingsLayout triggers this on mount so the rest of
// the tab strip is warm by the time the user clicks across.
export const settingsRoutePrefetch: readonly RouteImporter[] = [
  importSettingsAuth,
  importSettingsMiningPools,
  importSettingsTeam,
  importSettingsFirmware,
  importSettingsSchedules,
  importSettingsApiKeys,
];

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
 * Auth configuration - which routes require authentication
 */
export const requiresAuth: Record<string, boolean> = {
  "/auth": false,
  "/welcome": false,
  "/update-password": true, // Requires auth but is a special intermediate step
  "/fleet-down": false, // Error page doesn't require auth
  // All other routes require auth by default
};

/**
 * Router configuration - defines actual route tree with React elements
 */
const router = createBrowserRouter([
  // Dashboard (Home)
  createRoute("/", <Dashboard />, { bg: "surface-5" }),

  // Miners
  createRoute("/miners", <Miners />),

  // Groups
  createRoute("/groups", <GroupsPage />),
  createRoute("/groups/:groupLabel", <GroupOverviewPage />, { bg: "surface-5" }),

  // Racks
  createRoute("/racks", <RacksPage />),
  createRoute("/racks/:rackId", <RackOverviewPage />, { bg: "surface-5" }),

  // Activity
  createRoute("/activity", <ActivityPage />),

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
    "/settings/api-keys",
    <SettingsLayout>
      <SettingsApiKeys />
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
