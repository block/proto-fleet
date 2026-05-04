import { type ReactNode, useCallback, useState } from "react";

import { type DropdownOption } from "./DropdownFilter";
import FilterChip from "./FilterChip";
import NestedDropdownFilter, { type FilterCategory } from "./NestedDropdownFilter";
import { Plus } from "@/shared/assets/icons";

export type FilterChipsBarFilter = {
  key: string;
  title: string;
  pluralTitle?: string;
  options: DropdownOption[];
  selectedValues: string[];
};

type FilterChipsBarProps = {
  filters: FilterChipsBarFilter[];
  onChange: (key: string, selectedValues: string[]) => void;
  onClearAll?: () => void;
  triggerLabel?: string;
  triggerPrefixIcon?: ReactNode;
  triggerTestId?: string;
};

const FilterChipsBar = ({
  filters,
  onChange,
  onClearAll,
  triggerLabel = "Add Filter",
  triggerPrefixIcon = <Plus width="w-3" />,
  triggerTestId = "filter-nested-add-filter",
}: FilterChipsBarProps) => {
  // Tracking the open chip keeps it mounted while the user toggles its last selection off
  // — otherwise the chip unmounts mid-interaction and takes its popover with it.
  const [openChipKey, setOpenChipKey] = useState<string | null>(null);

  const fallbackClearAll = useCallback(() => {
    filters.forEach((f) => {
      if (f.selectedValues.length > 0) onChange(f.key, []);
    });
    setOpenChipKey(null);
  }, [filters, onChange]);

  const categories: FilterCategory[] = filters.map((f) => ({
    kind: "checkbox",
    key: f.key,
    label: f.title,
    options: f.options,
    selectedValues: f.selectedValues,
  }));

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
      <NestedDropdownFilter
        testId={triggerTestId}
        label={triggerLabel}
        prefixIcon={triggerPrefixIcon}
        categories={categories}
        onCheckboxChange={onChange}
        onRequestEdit={() => {
          // FilterChipsBar is checkbox-only; the trigger drilldown closes via
          // onCheckboxChange. onRequestEdit is reachable in theory but no path
          // here exposes a numeric/textareaList category, so a no-op is fine.
        }}
        onClearAll={onClearAll ?? fallbackClearAll}
      />
    </>
  );
};

export default FilterChipsBar;
