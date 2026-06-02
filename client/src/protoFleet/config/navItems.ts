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
          // SitesPage renders site CRUD with no view-only mode, so gate
          // the nav on site:manage to match the page's capability
          // rather than the list RPC's site:read. Same shape as the
          // Pools and Schedules secondary-nav entries.
          requiredPermission: "site:manage",
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
    // ActivityService is still pending its activity:read catalog key
    // (tracked separately). Once the server-side gating lands, gate
    // this nav entry on activity:read to mirror the RPC gate.
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
    // The Pools settings page is a management surface (Add / Edit /
    // Test / Delete with no read-only mode), so gate the nav on
    // pool:manage to match the page's capability rather than pool:read.
    // Read-only-pool custom roles get no useful UI here today.
    requiredPermission: "pool:manage",
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
    // The Schedules settings page is a management surface (Add, edit,
    // pause, resume, delete, reorder; no view-only mode), so gate the
    // nav on schedule:manage to match the page's capability rather
    // than schedule:read.
    requiredPermission: "schedule:manage",
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
