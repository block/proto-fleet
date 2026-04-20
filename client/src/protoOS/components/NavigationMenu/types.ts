import { type navigationItems, navigationMenuTypes } from "./constants";

export type NavigationMenuType = keyof typeof navigationMenuTypes;

export type NavigationItemKey = keyof typeof navigationItems;
export type NavigationItemValue = (typeof navigationItems)[keyof typeof navigationItems];
