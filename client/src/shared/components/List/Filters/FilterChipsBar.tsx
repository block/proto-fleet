import { type ReactNode, useCallback, useState } from "react";

import { type DropdownOption } from "./DropdownFilter";
import FilterChip from "./FilterChip";
import ModalFilterChip from "./ModalFilterChip";
import NestedDropdownFilter, { type FilterCategory } from "./NestedDropdownFilter";
import NumericRangeModal from "./NumericRangeModal";
import { Plus } from "@/shared/assets/icons";
import { formatNumericRangeCondition } from "@/shared/utils/filterChipFormatting";
import type { NumericRangeBounds, NumericRangeValue } from "@/shared/utils/filterValidation";

export type FilterChipsBarFilter = {
  key: string;
  title: string;
  pluralTitle?: string;
  options: DropdownOption[];
  selectedValues: string[];
  showGroupDivider?: boolean;
};

export type FilterChipsBarNumericFilter = {
  key: string;
  title: string;
  bounds: NumericRangeBounds;
  showGroupDivider?: boolean;
};

type FilterChipsBarProps = {
  filters: FilterChipsBarFilter[];
  onChange: (key: string, selectedValues: string[]) => void;
  numericFilters?: FilterChipsBarNumericFilter[];
  selectedNumericValues?: Record<string, NumericRangeValue>;
  onNumericChange?: (key: string, value: NumericRangeValue) => void;
  onClearAll?: () => void;
  triggerLabel?: string;
  triggerPrefixIcon?: ReactNode;
  triggerTestId?: string;
};

const FilterChipsBar = ({
  filters,
  onChange,
  numericFilters = [],
  selectedNumericValues = {},
  onNumericChange,
  onClearAll,
  triggerLabel = "Add Filter",
  triggerPrefixIcon = <Plus width="w-3" />,
  triggerTestId = "filter-nested-add-filter",
}: FilterChipsBarProps) => {
  // Tracking the open chip keeps it mounted while the user toggles its last selection off
  // — otherwise the chip unmounts mid-interaction and takes its popover with it.
  const [openChipKey, setOpenChipKey] = useState<string | null>(null);
  const [editingNumericKey, setEditingNumericKey] = useState<string | null>(null);

  const fallbackClearAll = useCallback(() => {
    filters.forEach((f) => {
      if (f.selectedValues.length > 0) onChange(f.key, []);
    });
    numericFilters.forEach((f) => {
      const value = selectedNumericValues[f.key];
      if (value?.min !== undefined || value?.max !== undefined) onNumericChange?.(f.key, {});
    });
    setOpenChipKey(null);
    setEditingNumericKey(null);
  }, [filters, numericFilters, onChange, onNumericChange, selectedNumericValues]);

  const categories: FilterCategory[] = [
    ...filters.map((f) => ({
      kind: "checkbox" as const,
      key: f.key,
      label: f.title,
      options: f.options,
      selectedValues: f.selectedValues,
      showGroupDivider: f.showGroupDivider,
    })),
    ...numericFilters.map((f) => ({
      kind: "numericRange" as const,
      key: f.key,
      label: f.title,
      bounds: f.bounds,
      value: selectedNumericValues[f.key] ?? {},
      showGroupDivider: f.showGroupDivider,
    })),
  ];

  const editingNumeric = editingNumericKey ? numericFilters.find((f) => f.key === editingNumericKey) : undefined;

  return (
    <>
      {filters.map((f) =>
        f.selectedValues.length > 0 || openChipKey === f.key ? (
          <FilterChip
            key={f.key}
            filterValue={f.key}
            title={f.title}
            pluralTitle={f.pluralTitle}
            options={f.options}
            selectedIds={f.selectedValues}
            onChange={(ids) => onChange(f.key, ids)}
            onClear={() => {
              onChange(f.key, []);
              setOpenChipKey((prev) => (prev === f.key ? null : prev));
            }}
            onOpenChange={(open) =>
              setOpenChipKey((prev) => {
                if (open) return f.key;
                return prev === f.key ? null : prev;
              })
            }
          />
        ) : null,
      )}
      {numericFilters.map((f) => {
        const value = selectedNumericValues[f.key];
        const condition = value ? formatNumericRangeCondition(value, f.bounds.unit) : "";
        return condition ? (
          <ModalFilterChip
            key={f.key}
            filterValue={f.key}
            typeLabel={f.title}
            condition={condition}
            onEdit={() => setEditingNumericKey(f.key)}
            onClear={() => onNumericChange?.(f.key, {})}
          />
        ) : null;
      })}
      <NestedDropdownFilter
        testId={triggerTestId}
        label={triggerLabel}
        prefixIcon={triggerPrefixIcon}
        categories={categories}
        onCheckboxChange={onChange}
        onRequestEdit={setEditingNumericKey}
        onClearAll={onClearAll ?? fallbackClearAll}
      />
      {editingNumeric ? (
        <NumericRangeModal
          open
          categoryKey={editingNumeric.key}
          label={editingNumeric.title}
          bounds={editingNumeric.bounds}
          initialValue={selectedNumericValues[editingNumeric.key] ?? {}}
          onApply={(value) => onNumericChange?.(editingNumeric.key, value)}
          onClose={() => setEditingNumericKey(null)}
        />
      ) : null}
    </>
  );
};

export default FilterChipsBar;
