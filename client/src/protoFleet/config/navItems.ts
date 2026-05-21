import { type ReactNode } from "react";

import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { Activity, Fleet, Groups, Home, IconProps, Racks, Settings, Site } from "@/shared/assets/icons";

export interface NavItem {
  path: string;
  label: string;
  icon?: (i: IconProps) => ReactNode;
  allowedRoles?: string[];
}

export interface SecondaryNavItem {
  path: string;
  label: string;
  parent: string;
  allowedRoles?: string[];
}

// Primary navigation items (shown in main nav menu)
export const primaryNavItems: NavItem[] = [
  {
    path: "/",
    label: "Home",
    icon: Home,
  },
  ...(MULTI_SITE_ENABLED
    ? [
        {
          path: "/sites",
          label: "Sites",
          icon: Site,
          // Backing RPCs (ListSites, ListBuildings, GetBuilding) are
          // admin-gated server-side. Mirror the gating client-side so
          // VIEWER doesn't land on a page that's guaranteed to fail.
          allowedRoles: ["SUPER_ADMIN", "ADMIN"],
        },
      ]
    : []),
  {
    path: "/miners",
    label: "Miners",
    icon: Fleet,
  },
  {
    path: "/racks",
    label: "Racks",
    icon: Racks,
  },
  {
    path: "/groups",
    label: "Groups",
    icon: Groups,
  },
  {
    path: "/activity",
    label: "Activity",
    icon: Activity,
  },
  {
    path: "/settings",
    label: "Settings",
    icon: Settings,
  },
];

// Secondary navigation items (shown in settings submenu)
export const secondaryNavItems: SecondaryNavItem[] = [
  {
    path: "/settings/general",
    label: "General",
    parent: "/settings",
  },
  {
    path: "/settings/security",
    label: "Security",
    parent: "/settings",
  },
  {
    path: "/settings/team",
    label: "Team",
    parent: "/settings",
  },
  {
    path: "/settings/mining-pools",
    label: "Pools",
    parent: "/settings",
  },
  {
    path: "/settings/firmware",
    label: "Firmware",
    parent: "/settings",
  },
  {
    path: "/settings/schedules",
    label: "Schedules",
    parent: "/settings",
  },
  {
    path: "/settings/api-keys",
    label: "API Keys",
    parent: "/settings",
    allowedRoles: ["SUPER_ADMIN", "ADMIN"],
  },
  ...(MULTI_SITE_ENABLED
    ? [
        {
          path: "/settings/sites",
          label: "Sites",
          parent: "/settings",
          // Site/building CRUD is admin-gated server-side; matching the
          // role restriction on adjacent admin-only entries (API Keys,
          // Server Logs) prevents VIEWER from landing on the page and
          // hitting PermissionDenied on every RPC.
          allowedRoles: ["SUPER_ADMIN", "ADMIN"],
        },
      ]
    : []),
  {
    path: "/settings/server-logs",
    label: "Server Logs",
    parent: "/settings",
    allowedRoles: ["SUPER_ADMIN", "ADMIN"],
  },
];
