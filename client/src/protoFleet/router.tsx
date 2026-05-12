/* eslint-disable react-refresh/only-export-components -- lazy() route components colocated with route config; not HMR-relevant */
import { createElement, lazy, ReactNode } from "react";
import { createBrowserRouter, LoaderFunction, Outlet, redirect } from "react-router-dom";

import App from "./components/App";
import SingleMinerWrapper from "./components/SingleMinerWrapper";
import type { PageBackground } from "./hooks/usePageBackground";
import { onboardingClient } from "@/protoFleet/api/clients";
// eslint-disable-next-line no-restricted-imports -- Fleet shell embeds the protoOS single-miner experience
import { routerConfig as singleMinerRoutes } from "@/protoOS/router";

// Route components are lazy-loaded so each route ships in its own chunk and
// only what's needed for first paint is in the entry bundle.
const Dashboard = lazy(() => import("@/protoFleet/features/dashboard/pages/Dashboard"));
const Miners = lazy(() => import("./features/fleetManagement/components/Fleet"));
const ActivityPage = lazy(() => import("@/protoFleet/features/activity/pages/ActivityPage"));
const ServerLogsPage = lazy(() => import("@/protoFleet/features/serverLogs/pages/ServerLogsPage"));
const GroupsPage = lazy(() => import("@/protoFleet/features/groupManagement/pages/GroupsPage"));
const GroupOverviewPage = lazy(() => import("@/protoFleet/features/groupManagement/pages/GroupOverviewPage"));
const RacksPage = lazy(() => import("@/protoFleet/features/rackManagement/pages/RacksPage"));
const RackOverviewPage = lazy(() => import("@/protoFleet/features/rackManagement/pages/RackOverviewPage"));
const Auth = lazy(() => import("@/protoFleet/features/auth/pages/Auth"));
const UpdatePassword = lazy(() => import("@/protoFleet/features/auth/pages/UpdatePassword"));
const WelcomePage = lazy(() => import("@/protoFleet/features/onboarding/components/Welcome"));
const MinersPage = lazy(() => import("@/protoFleet/features/onboarding/components/Miners"));
const SecurityPage = lazy(() => import("@/protoFleet/features/onboarding/components/Security"));
const OnboardingSettingsPage = lazy(() => import("@/protoFleet/features/onboarding/components/Settings"));
const SettingsLayout = lazy(() => import("@/protoFleet/features/settings/components/SettingsLayout"));
const SettingsGeneral = lazy(() => import("@/protoFleet/features/settings/components/General"));
const SettingsAuth = lazy(() => import("@/protoFleet/features/settings/components/Auth"));
const SettingsMiningPools = lazy(() => import("@/protoFleet/features/settings/components/MiningPools"));
const SettingsTeam = lazy(() => import("@/protoFleet/features/settings/components/Team"));
const SettingsFirmware = lazy(() => import("@/protoFleet/features/settings/components/Firmware"));
const SettingsSchedules = lazy(() => import("@/protoFleet/features/settings/components/Schedules/SchedulesPage"));
const SettingsApiKeys = lazy(() => import("@/protoFleet/features/settings/components/ApiKeys"));
const FleetDown = lazy(() => import("@/protoFleet/components/FleetDown/FleetDown"));

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
