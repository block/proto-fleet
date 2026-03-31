import { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

export type DropdownOption = {
  id: string;
  label: string;
};

export type FilterType = "button" | "dropdown";

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

export type FilterItem = ButtonFilterItem | DropdownFilterItem;

export type ActiveFilters = {
  buttonFilters: string[];
  dropdownFilters: Record<string, string[]>;
};
