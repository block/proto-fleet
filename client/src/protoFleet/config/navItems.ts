import { type ReactNode } from "react";

import { Fleet, Home, IconProps, Settings } from "@/shared/assets/icons";

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
    path: "/settings/mining-pools",
    label: "Mining Pools",
    parent: "/settings",
  },
];
