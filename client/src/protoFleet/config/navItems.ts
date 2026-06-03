import { type ReactNode } from "react";

import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { Activity, Fleet, Groups, Home, IconProps, LightningAlt, Racks, Settings } from "@/shared/assets/icons";

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
          // Multi-site redesign (2026-06-02) collapses /miners, /racks,
          // /sites, and /settings/sites into a single tabbed Fleet page.
          // Behind the same flag that previously hid the /sites nav entry.
          path: "/fleet",
          label: "Fleet",
          icon: Fleet,
        },
      ]
    : [
        // Pre-redesign: standalone Miners + Racks entries. Removed once the
        // flag flips on. The /miners and /racks routes themselves are
        // permanent redirects to /fleet/miners and /fleet/racks regardless
        // of the flag — see router.tsx.
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
      ]),
  {
    path: "/groups",
    label: "Groups",
    icon: Groups,
  },
  {
    path: "/energy",
    label: "Energy",
    icon: LightningAlt,
    requiredPermission: "curtailment:read",
  },
  {
    path: "/activity",
    label: "Activity",
    icon: Activity,
    // ActivityService is server-gated on activity:read (PR #347).
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
  // /settings/sites was removed by the 2026-06-02 multi-site redesign —
  // site config now lives on /sites/:id detail pages reached via the
  // Sites tab on /fleet. The /settings/sites route itself stays as an
  // unguarded redirect path until traffic dies down.
  {
    path: "/settings/server-logs",
    label: "Server Logs",
    parent: "/settings",
    requiredPermission: "serverlog:read",
  },
];
