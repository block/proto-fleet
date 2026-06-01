import { type ReactNode } from "react";

import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { Activity, Fleet, Groups, Home, IconProps, Racks, Settings, Site } from "@/shared/assets/icons";

export interface NavItem {
  path: string;
  label: string;
  icon?: (i: IconProps) => ReactNode;
  // Catalog permission key the caller must hold to see this entry. Mirrors
  // the server-side gate on the page's backing RPCs; consumers filter via
  // useHasPermission. Entries without a requiredPermission are visible to
  // every authenticated user.
  requiredPermission?: string;
}

export interface SecondaryNavItem {
  path: string;
  label: string;
  parent: string;
  requiredPermission?: string;
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
          // Sites listing reads call ListSites/ListBuildings/GetBuilding,
          // all server-gated on site:read.
          requiredPermission: "site:read",
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
    // ActivityService is server-gated on activity:read.
    requiredPermission: "activity:read",
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
    requiredPermission: "pool:read",
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
    requiredPermission: "schedule:read",
  },
  {
    path: "/settings/api-keys",
    label: "API Keys",
    parent: "/settings",
    requiredPermission: "apikey:manage",
  },
  ...(MULTI_SITE_ENABLED
    ? [
        {
          path: "/settings/sites",
          label: "Sites",
          parent: "/settings",
          requiredPermission: "site:manage",
        },
      ]
    : []),
  {
    path: "/settings/server-logs",
    label: "Server Logs",
    parent: "/settings",
    requiredPermission: "serverlog:read",
  },
];
