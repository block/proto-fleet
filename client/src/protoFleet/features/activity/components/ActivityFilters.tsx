import { type ReactNode, useCallback, useMemo } from "react";

import type { EventTypeOption, UserOption } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { baseEventType } from "@/protoFleet/features/activity/utils/eventType";
import { formatActivityFilterLabel, formatLabel } from "@/protoFleet/features/activity/utils/formatLabel";
import Input from "@/shared/components/Input";
import FilterChipsBar, { type FilterChipsBarFilter } from "@/shared/components/List/Filters/FilterChipsBar";

const EVENT_TYPE_CATEGORY_ORDER = [
  "auth",
  "device_command",
  "fleet_management",
  "collection",
  "pool",
  "schedule",
  "curtailment",
  "system",
];

type ActivityTypeFilterOption = {
  id: string;
  label: string;
  rawEventTypes: string[];
  category: string;
  showGroupDivider?: boolean;
};

type ActivityTypeFilterOptionGroup = ActivityTypeFilterOption & {
  firstIndex: number;
};

const categorySortValue = (category: string): number => {
  const index = EVENT_TYPE_CATEGORY_ORDER.indexOf(category);
  return index === -1 ? EVENT_TYPE_CATEGORY_ORDER.length : index;
};

const optionGroupKey = (category: string, label: string): string => `${category || "other"}:${label.toLowerCase()}`;

const buildTypeOptions = (eventTypes: EventTypeOption[]): ActivityTypeFilterOption[] => {
  const optionByGroup = new Map<string, ActivityTypeFilterOptionGroup>();

  eventTypes.forEach((eventTypeOption, index) => {
    const rawEventType = eventTypeOption.eventType;
    if (!rawEventType) return;

    const category = eventTypeOption.eventCategory;
    const label = formatActivityFilterLabel(rawEventType);
    const key = optionGroupKey(category, label);
    const existing = optionByGroup.get(key);

    if (existing) {
      if (!existing.rawEventTypes.includes(rawEventType)) {
        existing.rawEventTypes.push(rawEventType);
      }
      return;
    }

    optionByGroup.set(key, {
      id: baseEventType(rawEventType),
      label,
      rawEventTypes: [rawEventType],
      category,
      firstIndex: index,
    });
  });

  const options = Array.from(optionByGroup.values()).sort((a, b) => {
    const categoryDelta = categorySortValue(a.category) - categorySortValue(b.category);
    if (categoryDelta !== 0) return categoryDelta;
    return a.firstIndex - b.firstIndex;
  });

  return options.map(({ firstIndex: _firstIndex, ...option }, index) => ({
    ...option,
    showGroupDivider: index < options.length - 1 && option.category !== options[index + 1].category,
  }));
};

interface ActivityFiltersProps {
  searchValue: string;
  onSearchChange: (value: string) => void;
  eventTypes: EventTypeOption[];
  scopeTypes: string[];
  users: UserOption[];
  selectedTypes: string[];
  selectedScopes: string[];
  selectedUsers: string[];
  onTypesChange: (types: string[]) => void;
  onScopesChange: (scopes: string[]) => void;
  onUsersChange: (users: string[]) => void;
  actions?: ReactNode;
}

const ActivityFilters = ({
  searchValue,
  onSearchChange,
  eventTypes,
  scopeTypes,
  users,
  selectedTypes,
  selectedScopes,
  selectedUsers,
  onTypesChange,
  onScopesChange,
  onUsersChange,
  actions,
}: ActivityFiltersProps) => {
  const typeOptions = useMemo(() => buildTypeOptions(eventTypes), [eventTypes]);

  const selectedTypeOptionIds = useMemo(() => {
    const selectedRawTypes = new Set(selectedTypes);
    return typeOptions
      .filter((option) => option.rawEventTypes.some((rawEventType) => selectedRawTypes.has(rawEventType)))
      .map((option) => option.id);
  }, [selectedTypes, typeOptions]);

  const scopeOptions = useMemo(() => scopeTypes.map((st) => ({ id: st, label: formatLabel(st) })), [scopeTypes]);

  const userOptions = useMemo(() => users.map((u) => ({ id: u.userId, label: u.username })), [users]);

  const filterChipsBarFilters = useMemo<FilterChipsBarFilter[]>(() => {
    const filters: FilterChipsBarFilter[] = [];

    if (typeOptions.length > 0) {
      filters.push({
        key: "type",
        title: "Type",
        pluralTitle: "types",
        options: typeOptions,
        selectedValues: selectedTypeOptionIds,
      });
    }

    if (scopeOptions.length > 0) {
      filters.push({
        key: "scope",
        title: "Scope",
        pluralTitle: "scopes",
        options: scopeOptions,
        selectedValues: selectedScopes,
      });
    }

    if (userOptions.length > 0) {
      filters.push({
        key: "users",
        title: "Users",
        pluralTitle: "users",
        options: userOptions,
        selectedValues: selectedUsers,
      });
    }

    return filters;
  }, [scopeOptions, selectedScopes, selectedTypeOptionIds, selectedUsers, typeOptions, userOptions]);

  const handleFilterChange = useCallback(
    (key: string, selectedValues: string[]) => {
      if (key === "type") {
        const selectedOptionIds = new Set(selectedValues);
        onTypesChange(typeOptions.flatMap((option) => (selectedOptionIds.has(option.id) ? option.rawEventTypes : [])));
      } else if (key === "scope") {
        onScopesChange(selectedValues);
      } else if (key === "users") {
        onUsersChange(selectedValues);
      }
    },
    [onScopesChange, onTypesChange, onUsersChange, typeOptions],
  );

  const handleClearAllFilters = useCallback(() => {
    onTypesChange([]);
    onScopesChange([]);
    onUsersChange([]);
  }, [onScopesChange, onTypesChange, onUsersChange]);

  const handleClearSearch = useCallback(
    (key: string) => {
      if (key === "Escape") {
        onSearchChange("");
      }
    },
    [onSearchChange],
  );

  return (
    <div className="flex w-full min-w-0 flex-col gap-2">
      <div className="w-full min-w-0" data-testid="activity-search-row">
        <Input
          id="activity-search"
          label="Search activity"
          hideLabelOnFocus
          className="!h-8 !rounded-3xl"
          initValue={searchValue}
          onChange={(value) => onSearchChange(value)}
          onKeyDown={handleClearSearch}
        />
      </div>
      <div
        className="flex w-full min-w-0 flex-wrap items-center justify-between gap-2"
        data-testid="activity-toolbar-row"
      >
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          {filterChipsBarFilters.length > 0 ? (
            <FilterChipsBar
              filters={filterChipsBarFilters}
              onChange={handleFilterChange}
              onClearAll={handleClearAllFilters}
            />
          ) : null}
        </div>
        {actions ? <div className="ml-auto shrink-0">{actions}</div> : null}
      </div>
    </div>
  );
};

export default ActivityFilters;
