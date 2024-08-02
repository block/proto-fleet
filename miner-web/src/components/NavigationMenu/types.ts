import { type navigationItems } from "./constants";

export type NavigationItemKey = keyof typeof navigationItems;
export type NavigationItemValue =
  (typeof navigationItems)[keyof typeof navigationItems];
