import { type ReactNode } from "react";

import { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";
import type { NumericRangeBounds, NumericRangeValue } from "@/shared/utils/filterValidation";

export type { NumericRangeBounds, NumericRangeValue };

export type DropdownOption = {
  id: string;
  label: string;
};

export type FilterType = "button" | "dropdown" | "nestedFilterDropdown" | "numericRange" | "textareaList";

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
 * Numeric range filter — renders min/max inputs as a nested submenu row.
 * `value` and `unit` are display units (TH/s, kW, J/TH, °C) so a filter input
 * matches what the telemetry APIs emit. Inclusivity is hard-wired to inclusive
 * for v1; the server already exposes flags if we ever want exclusive.
 */
export type NumericRangeFilterItem = BaseFilterItem & {
  type: "numericRange";
  bounds: NumericRangeBounds;
};

/**
 * Free-form line-list filter (textarea, one entry per line). Domain-agnostic:
 * callers pass per-line `validate` / optional `normalize` so the same shape
 * powers Subnet today and could power MAC allowlists or worker-name filters
 * later. `maxLines` defaults to 1024 to match the server cap.
 */
export type TextareaListFilterItem = BaseFilterItem & {
  type: "textareaList";
  validate: (line: string) => string | null;
  normalize?: (line: string) => string;
  placeholder?: string;
  maxLines?: number;
  /**
   * Singular noun used in the chip summary when there are multiple entries
   * (e.g. "subnet" → "3 subnets"). Falls back to "entries" when omitted.
   */
  noun?: string;
};

/**
 * A meta-dropdown trigger whose popover lists each child as a row that opens its
 * own nested submenu. Children share the same active-state keys as any standalone
 * `dropdown` items with matching `value`, so the two surfaces stay in sync.
 */
export type NestedFilterDropdownItem = BaseFilterItem & {
  type: "nestedFilterDropdown";
  children: NestedFilterChildItem[];
  // Optional icon rendered to the left of the trigger label. When provided the
  // trigger drops its chevron suffix so the icon-led action style ("+ Add Filter")
  // reads as a button instead of a select.
  prefixIcon?: ReactNode;
};

export type NestedFilterChildItem = DropdownFilterItem | NumericRangeFilterItem | TextareaListFilterItem;

export type FilterItem = ButtonFilterItem | DropdownFilterItem | NestedFilterDropdownItem;

/**
 * Submenu category passed into `NestedDropdownFilter`. Discriminated by
 * `kind` so each row can render its own submenu component (checkbox list,
 * min/max inputs, textarea). Keeps the props strongly typed without a render
 * prop.
 */
export type FilterCategoryBase = { key: string; label: string };

export type CheckboxFilterCategory = FilterCategoryBase & {
  kind: "checkbox";
  options: DropdownOption[];
  selectedValues: string[];
};

export type NumericRangeFilterCategory = FilterCategoryBase & {
  kind: "numericRange";
  bounds: NumericRangeBounds;
  value: NumericRangeValue;
};

export type TextareaListFilterCategory = FilterCategoryBase & {
  kind: "textareaList";
  validate: (line: string) => string | null;
  normalize?: (line: string) => string;
  placeholder?: string;
  maxLines?: number;
  value: string[];
};

export type FilterCategory = CheckboxFilterCategory | NumericRangeFilterCategory | TextareaListFilterCategory;

export type ActiveFilters = {
  buttonFilters: string[];
  dropdownFilters: Record<string, string[]>;
  // Numeric range filters (hashrate, efficiency, power, temperature). Empty
  // value (`{}`) means "no filter for this key" — the entry should be removed
  // from the record entirely so iteration over Object.entries skips it.
  numericFilters: Record<string, NumericRangeValue>;
  // Free-form line-list filters (Subnet today, future MAC/serial/worker lists).
  // Stored values are the post-normalize, post-dedup set the server should
  // receive — never the raw textarea contents.
  textareaListFilters: Record<string, string[]>;
};
