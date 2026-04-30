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
};

/**
 * A meta-dropdown trigger whose popover lists each child as a row that opens its
 * own nested submenu. Children share the same active-state keys as any standalone
 * `dropdown` items with matching `value`, so the two surfaces stay in sync.
 */
export type NestedFilterDropdownItem = BaseFilterItem & {
  type: "nestedFilterDropdown";
  children: DropdownFilterItem[];
};

export type FilterItem = ButtonFilterItem | DropdownFilterItem | NestedFilterDropdownItem;

export type ActiveFilters = {
  buttonFilters: string[];
  dropdownFilters: Record<string, string[]>;
};
