import { type ReactNode } from "react";

import { Activity, Fleet, Groups, Home, IconProps, LightningAlt, Settings } from "@/shared/assets/icons";

// Runtime-gated features: an entry tagged with one is shown only when the server
// reports the feature enabled (see SecondaryNavigation). Distinct from
// requiredPermission, which is a per-user capability the client already knows.
export type NavFeature = "notifications";

export interface NavItem {
  path: string;
  label: string;
  icon?: (i: IconProps) => ReactNode;
  // Catalog permission key the caller must hold to see this entry. Mirrors
  // the server-side gate on the page's backing RPCs; consumers filter via
  // useHasPermission. Entries without a requiredPermission are visible to
  // every authenticated user.
  requiredPermission?: string;
  scopable?: boolean;
}

export interface SecondaryNavItem {
  path: string;
  label: string;
  parent: string;
  requiredPermission?: string;
  // When set, the entry is shown only if the server reports this feature enabled.
  requiredFeature?: NavFeature;
}

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
    // ListUsers is server-gated on user:read (held by ADMIN + SUPER_ADMIN
    // but not FIELD_TECH). Without this gate the entry shows for every
    // authenticated user even though the page can't load anything.
    requiredPermission: "user:read",
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
    path: "/settings/curtailment",
    label: "Curtailment",
    parent: "/settings",
    requiredPermission: "curtailment:manage",
  },
  {
    path: "/settings/api-keys",
    label: "API Keys",
    parent: "/settings",
    requiredPermission: "apikey:manage",
  },
  {
    path: "/settings/notifications",
    label: "Notifications",
    parent: "/settings",
    requiredPermission: "notification:read",
    // Needs the Grafana sidecar, which is off in the default deployment. Gated
    // at runtime so an operator enabling the sidecar surfaces the entry without
    // a client rebuild.
    requiredFeature: "notifications",
  },
  {
    path: "/settings/server-logs",
    label: "Server Logs",
    parent: "/settings",
    requiredPermission: "serverlog:read",
  },
];
