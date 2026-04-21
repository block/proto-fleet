import { useCallback, useMemo } from "react";

import type { EventTypeOption, UserOption } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { formatLabel } from "@/protoFleet/features/activity/utils/formatLabel";
import Input from "@/shared/components/Input";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";

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
}: ActivityFiltersProps) => {
  const typeOptions = useMemo(
    () => eventTypes.map((et) => ({ id: et.eventType, label: formatLabel(et.eventType) })),
    [eventTypes],
  );

  const scopeOptions = useMemo(() => scopeTypes.map((st) => ({ id: st, label: formatLabel(st) })), [scopeTypes]);

  const userOptions = useMemo(() => users.map((u) => ({ id: u.userId, label: u.username })), [users]);

  const handleClearSearch = useCallback(
    (key: string) => {
      if (key === "Escape") {
        onSearchChange("");
      }
    },
    [onSearchChange],
  );

  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="w-56 phone:w-full">
        <Input
          id="activity-search"
          label="Search activity..."
          hideLabelOnFocus
          className="!h-8 !rounded-3xl"
          initValue={searchValue}
          onChange={(value) => onSearchChange(value)}
          onKeyDown={handleClearSearch}
        />
      </div>
      {typeOptions.length > 0 && (
        <DropdownFilter
          title="Type"
          options={typeOptions}
          selectedOptions={selectedTypes}
          onSelect={onTypesChange}
          withButtons
        />
      )}
      {scopeOptions.length > 0 && (
        <DropdownFilter
          title="Scope"
          options={scopeOptions}
          selectedOptions={selectedScopes}
          onSelect={onScopesChange}
          withButtons
        />
      )}
      {userOptions.length > 0 && (
        <DropdownFilter
          title="Users"
          options={userOptions}
          selectedOptions={selectedUsers}
          onSelect={onUsersChange}
          withButtons
        />
      )}
    </div>
  );
};

export default ActivityFilters;
