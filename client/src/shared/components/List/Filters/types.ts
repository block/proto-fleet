import { type ReactNode } from "react";

import { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

export type DropdownOption = {
  id: string;
  label: string;
};

export type FilterType = "button" | "dropdown" | "nestedFilterDropdown";

export type BaseFilterItem = {
  title: string;
  value: string;
  type: FilterType;
};

export type ButtonFilterItem = BaseFilterItem & {
  type: "button";
  status?: StatusCircleStatus;
  count: number;
};

export type DropdownFilterItem = BaseFilterItem & {
  type: "dropdown";
  options: DropdownOption[];
  defaultOptionIds: string[];
  showSelectAll?: boolean;
  // Plural form of `title` used in active-filter chips when multiple options are selected
  // (e.g. "3 statuses"). Defaults to `title + "s"` if omitted, which is wrong for
  // irregular plurals like "Status".
  pluralTitle?: string;
};

/**
 * A meta-dropdown trigger whose popover lists each child as a row that opens its
 * own nested submenu. Children share the same active-state keys as any standalone
 * `dropdown` items with matching `value`, so the two surfaces stay in sync.
 */
export type NestedFilterDropdownItem = BaseFilterItem & {
  type: "nestedFilterDropdown";
  children: DropdownFilterItem[];
  // Optional icon rendered to the left of the trigger label. When provided the
  // trigger drops its chevron suffix so the icon-led action style ("+ Add Filter")
  // reads as a button instead of a select.
  prefixIcon?: ReactNode;
};

export type FilterItem = ButtonFilterItem | DropdownFilterItem | NestedFilterDropdownItem;

export type ActiveFilters = {
  buttonFilters: string[];
  dropdownFilters: Record<string, string[]>;
};
