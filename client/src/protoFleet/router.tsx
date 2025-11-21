import { createElement, ReactNode } from "react";
import { createBrowserRouter, Outlet, redirect } from "react-router-dom";

import App from "./components/App";
import SingleMinerWrapper from "./components/SingleMinerWrapper";
import Miners from "./features/fleetManagement/components/Fleet";
import DashboardLayout from "@/protoFleet/components/DashboardLayout/DashboardLayout";
import Auth from "@/protoFleet/features/auth/pages/Auth";
import UpdatePassword from "@/protoFleet/features/auth/pages/UpdatePassword";
import {
  MinersPage,
  SecurityPage,
  SettingsPage,
  WelcomePage,
} from "@/protoFleet/features/onboarding";
import {
  Authentication as AuthSettings,
  General,
  MiningPools,
  SettingsLayout,
  Team,
} from "@/protoFleet/features/settings";
import { routerConfig as singleMinerRoutes } from "@/protoOS/router";

// Helper to create route objects with App wrapper
interface CreateRouteOptions {
  fullscreen?: boolean;
}

const createRoute = (
  path: string,
  children: ReactNode,
  options: CreateRouteOptions = {},
) => ({
  path,
  element: <App fullscreen={options.fullscreen}>{children}</App>,
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
  // All other routes require auth by default
};

/**
 * Router configuration - defines actual route tree with React elements
 */
const router = createBrowserRouter([
  // Dashboard (Home)
  createRoute("/", <DashboardLayout />),

  // Miners
  createRoute("/miners", <Miners />),

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
      <General />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/security",
    <SettingsLayout>
      <AuthSettings />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/mining-pools",
    <SettingsLayout>
      <MiningPools />
    </SettingsLayout>,
  ),
  createRoute(
    "/settings/team",
    <SettingsLayout>
      <Team />
    </SettingsLayout>,
  ),

  // Auth routes (fullscreen)
  createRoute("/auth", <Auth />, { fullscreen: true }),
  createRoute("/update-password", <UpdatePassword />, { fullscreen: true }),
  createRoute("/welcome", <WelcomePage />, { fullscreen: true }),

  // Onboarding routes
  createRoute("/onboarding/miners", <MinersPage />),
  createRoute("/onboarding/security", <SecurityPage />, { fullscreen: true }),
  createRoute("/onboarding/settings", <SettingsPage />, { fullscreen: true }),
]);

export default router;
