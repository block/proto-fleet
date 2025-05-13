import { createElement, type ReactNode } from "react";
import { matchPath, redirect, type RouteObject } from "react-router-dom";

import SingleMinerWrapper from "./components/SingleMinerWrapper";
import Miners from "./features/fleetManagement/components/Fleet";
import HomePage from "./pages/Home";
import { Cooling, General, Hardware, MiningPools } from "./pages/Settings";
import Auth from "@/protoFleet/features/auth/pages/Auth";
import {
  AuthenticationPage,
  MinersPage,
  MiningPoolPage,
  NetworkPage,
  WelcomePage,
} from "@/protoFleet/features/onboarding";
import Signup from "@/protoFleet/pages/Signup";
import { routerConfig as singleMinerRoutes } from "@/protoOS/router";

import { Fleet, Home, IconProps, Settings } from "@/shared/assets/icons";

type Route = RouteObject & {
  label?: string;
  overrideLayout?: boolean;
  icon?: (i: IconProps) => ReactNode;
  navItem?: boolean;
  secondaryNavItem?: string;
  requireAuth?: boolean;
};

export type NavRoute = Omit<Route, "element">;

// copies all Proto OS routes and wraps their element in SingleMinerWrapper
const wrappedMinerRoutes = singleMinerRoutes.map((route) => {
  const wrappedElement = createElement(SingleMinerWrapper, null, route.element);

  return {
    ...route,
    overrideLayout: true,
    element: wrappedElement,
  };
});

const routes: Route[] = [
  {
    path: "/",
    label: "Home",
    icon: Home,
    navItem: true,
    element: <HomePage />,
  },
  {
    path: "/miners",
    label: "Miners",
    icon: Fleet,
    navItem: true,
    element: <Miners />,
  },
  {
    path: "/miners/:id",
    children: wrappedMinerRoutes,
    overrideLayout: true,
  },
  {
    path: "/settings",
    label: "Settings",
    icon: Settings,
    navItem: true,
    loader: () => redirect("/settings/general"),
  },
  {
    path: "settings/general",
    label: "General",
    secondaryNavItem: "/settings",
    element: <General />,
  },
  {
    path: "settings/hardware",
    label: "Hardware",
    secondaryNavItem: "/settings",
    element: <Hardware />,
  },
  {
    path: "settings/mining-pools",
    label: "Mining Pools",
    secondaryNavItem: "/settings",
    element: <MiningPools />,
  },
  {
    path: "settings/cooling",
    label: "Cooling",
    secondaryNavItem: "/settings",
    element: <Cooling />,
  },
  {
    path: "/auth",
    element: <Auth />,
    overrideLayout: true,
  },
  {
    path: "/signup",
    element: <Signup />,
    overrideLayout: true,
  },
  {
    path: "/onboarding",
    overrideLayout: true,
    requireAuth: false,
    loader: () => redirect("/onboarding/welcome"),
  },
  {
    path: "/onboarding/welcome",
    element: <WelcomePage />,
    requireAuth: false,
    overrideLayout: true,
  },
  {
    path: "/onboarding/authentication",
    element: <AuthenticationPage />,
    requireAuth: false,
    overrideLayout: true,
  },
  {
    path: "/onboarding/network",
    element: <NetworkPage />,
    overrideLayout: true,
  },
  {
    path: "/onboarding/miners",
    element: <MinersPage />,
    overrideLayout: true,
  },
  {
    path: "/onboarding/mining-pool",
    element: <MiningPoolPage />,
    overrideLayout: true,
  },
];

/**
 * Find the route in routeConfig that matches a pathname
 * and returns metadata associated with that route.
 *
 * Normally our routes would have the <App /> compomnent in then where
 * we could pass props like title, but because we use this same config
 * to construct the NavigationMenu and SecondaryNavigation, the reference
 * of App would cause a circular dependency.
 */

export const getRouteMetadata = (pathname: string) => {
  // find the route in routeConfig that matches a pathname
  const route = routes.find((r) => {
    if (!r.path) return false;
    return matchPath(r.path, pathname);
  });

  return {
    title: route?.label || "",
    requireAuth: route?.requireAuth,

    // only use AppLayout if route is defined and not explicitly overridden.
    // route will be undefined for the nested routes singleMinerRoutes
    useAppLayout: route && !route?.overrideLayout,
  };
};

export default routes;
