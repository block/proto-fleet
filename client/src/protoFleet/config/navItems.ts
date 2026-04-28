import { type ReactNode } from "react";

import { Activity, Fleet, Groups, Home, IconProps, Racks, Settings } from "@/shared/assets/icons";

export interface NavItem {
  path: string;
  label: string;
  icon?: (i: IconProps) => ReactNode;
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
  {
    // Server Logs is admin-only — same scope as API Keys, since the
    // buffered records can include any structured attrs the server's
    // slog calls attached (request ids, user ids, etc.).
    path: "/settings/server-logs",
    label: "Server Logs",
    parent: "/settings",
    allowedRoles: ["SUPER_ADMIN", "ADMIN"],
  },
];
