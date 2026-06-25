import { type ReactNode } from "react";

import { Activity, Fleet, Groups, Home, IconProps, LightningAlt, Settings } from "@/shared/assets/icons";

// Runtime-gated features: an entry tagged with one is shown only when the server
// reports the feature enabled (see SecondaryNavigation). Distinct from
// requiredPermission, which is a per-user capability the client already knows.
export type NavFeature = "alerts";

export interface NavItem {
  path: string;
  label: string;
  icon?: (i: IconProps) => ReactNode;
  // Catalog permission key the caller must hold to see this entry. Mirrors
  // the server-side gate on the page's org-scoped backing RPCs; consumers
  // filter via UserInfo.permissions. Entries without a requiredPermission
  // are visible to every authenticated user.
  requiredPermission?: string;
  requiredAnyPermission?: string[];
  scopable?: boolean;
}

export interface SecondaryNavItem {
  path: string;
  label: string;
  parent: string;
  section?: "Fleet" | "Automation" | "Admin" | "Account";
  requiredPermission?: string;
  requiredAnyPermission?: string[];
  // When set, the entry is shown only if the server reports this feature enabled.
  requiredFeature?: NavFeature;
}

export const isNavItemAllowedByPermissions = (
  item: Pick<NavItem | SecondaryNavItem, "requiredPermission" | "requiredAnyPermission">,
  permissions: readonly string[],
) => {
  const hasRequiredPermission = !item.requiredPermission || permissions.includes(item.requiredPermission);
  const hasAnyPermission =
    !item.requiredAnyPermission || item.requiredAnyPermission.some((permission) => permissions.includes(permission));

  return hasRequiredPermission && hasAnyPermission;
};

// Primary navigation items (shown in main nav menu)
export const primaryNavItems: NavItem[] = [
  {
    path: "/dashboard",
    label: "Home",
    icon: Home,
    scopable: true,
  },
  {
    path: "/fleet",
    label: "Fleet",
    icon: Fleet,
    scopable: true,
  },
  {
    path: "/groups",
    label: "Groups",
    icon: Groups,
    scopable: true,
  },
  {
    path: "/energy",
    label: "Energy",
    icon: LightningAlt,
    requiredPermission: "curtailment:read",
    scopable: true,
  },
  {
    path: "/activity",
    label: "Activity",
    icon: Activity,
    // ActivityService is server-gated on activity:read (PR #347).
    requiredPermission: "activity:read",
    scopable: true,
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
    path: "/settings/network",
    label: "Network",
    parent: "/settings",
    section: "Fleet",
  },
  {
    path: "/settings/mining-pools",
    label: "Pools",
    parent: "/settings",
    section: "Fleet",
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
    section: "Fleet",
  },
  {
    path: "/settings/schedules",
    label: "Schedules",
    parent: "/settings",
    section: "Automation",
    // The Schedules settings page is a management surface (Add, edit,
    // pause, resume, delete, reorder; no view-only mode), so gate the
    // nav on schedule:manage to match the page's capability rather
    // than schedule:read.
    requiredPermission: "schedule:manage",
  },
  {
    path: "/settings/curtailment",
    label: "Curtailment",
    parent: "/settings",
    section: "Automation",
    requiredPermission: "curtailment:manage",
  },
  {
    path: "/settings/alerts",
    label: "Alerts",
    parent: "/settings",
    section: "Automation",
    requiredPermission: "alert:read",
    // Needs the Grafana sidecar, which is off in the default deployment. Gated
    // at runtime so an operator enabling the sidecar surfaces the entry without
    // a client rebuild.
    requiredFeature: "alerts",
  },
  {
    path: "/settings/security",
    label: "Security",
    parent: "/settings",
    section: "Admin",
  },
  {
    path: "/settings/team",
    label: "Team",
    parent: "/settings",
    section: "Admin",
    // Team now owns member and role management. Show the entry when either
    // surface is usable so role-only admins are not stranded after the merge.
    requiredAnyPermission: ["user:read", "role:manage"],
  },
  {
    path: "/settings/integrations",
    label: "Integrations",
    parent: "/settings",
    section: "Admin",
    requiredPermission: "apikey:manage",
  },
  {
    path: "/settings/server-logs",
    label: "Server Logs",
    parent: "/settings",
    section: "Admin",
    requiredPermission: "serverlog:read",
  },
  {
    path: "/settings/preferences",
    label: "Preferences",
    parent: "/settings",
    section: "Account",
  },
];
