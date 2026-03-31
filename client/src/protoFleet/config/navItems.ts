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
    path: "/groups",
    label: "Groups",
    icon: Groups,
  },
  ...(import.meta.env.VITE_FEATURE_RACKS === "true"
    ? [
        {
          path: "/racks",
          label: "Racks",
          icon: Racks,
        },
      ]
    : []),
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
];
