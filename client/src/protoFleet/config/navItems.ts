import { type ReactNode } from "react";

import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { Activity, Fleet, Groups, Home, IconProps, LightningAlt, Racks, Settings, Site } from "@/shared/assets/icons";

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
          // Gate on site:read so any role that can list sites can
          // navigate to the overview. SitesPage renders the read view
          // for everyone (ListSites + ListBuildings, both site:read);
          // the "Add site" CTA and per-card edit/delete affordances
          // gate on site:manage independently inside the page, so
          // restricting the nav to site:manage would hide a useful
          // surface from read-only roles for no security benefit.
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
    path: "/settings/roles",
    label: "Roles",
    parent: "/settings",
    // Roles management reads/writes are server-gated on role:manage.
    requiredPermission: "role:manage",
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
