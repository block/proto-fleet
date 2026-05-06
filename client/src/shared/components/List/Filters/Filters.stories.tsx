import { useState } from "react";
import { action } from "storybook/actions";

import FiltersComponent from "./index";
import { defaultListFilter } from "@/shared/components/List/constants";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import { statuses } from "@/shared/components/StatusCircle";

const onFilterChange = action("Filter changed");

const buttonFilters: FilterItem[] = [
  {
    type: "button",
    title: "All Items",
    value: defaultListFilter,
    count: 25,
  },
  {
    type: "button",
    title: "Active",
    value: "active",
    count: 15,
    status: statuses.normal,
  },
  {
    type: "button",
    title: "Inactive",
    value: "inactive",
    count: 8,
    status: statuses.inactive,
  },
  {
    type: "button",
    title: "Warning",
    value: "warning",
    count: 2,
    status: statuses.warning,
  },
];

const statusOptions = [
  { id: "hashing", label: "Hashing" },
  { id: "offline", label: "Offline" },
  { id: "sleeping", label: "Sleeping" },
];

const typeOptions = [
  { id: "proto-rig", label: "Proto Rig" },
  { id: "bitmain", label: "Bitmain" },
  { id: "whatsminer", label: "Whatsminer" },
];

export const ClientSideFiltering = () => {
  const filters: FilterItem[] = [
    ...buttonFilters,
    {
      type: "dropdown",
      title: "Status",
      value: "status",
      options: statusOptions,
      defaultOptionIds: statusOptions.map((o) => o.id),
    },
    {
      type: "dropdown",
      title: "Type",
      value: "type",
      options: typeOptions,
      defaultOptionIds: typeOptions.map((o) => o.id),
    },
  ];

  return (
    <div className="flex flex-col gap-4 p-4">
      <FiltersComponent
        className="gap-4"
        filterItems={filters}
        items={[]}
        onFilter={(activeFilters) => {
          onFilterChange(activeFilters);
        }}
        isServerSide={false}
      />
      <div className="text-300">
        <p>
          <strong>Client-side filtering:</strong> Dropdown filters use immediate mode. Changes fire callbacks instantly
          without Apply/Reset buttons.
        </p>
      </div>
    </div>
  );
};

export const ServerSideFiltering = () => {
  const filters: FilterItem[] = [
    ...buttonFilters,
    {
      type: "dropdown",
      title: "Status",
      value: "status",
      options: statusOptions,
      defaultOptionIds: statusOptions.map((o) => o.id),
    },
    {
      type: "dropdown",
      title: "Type",
      value: "type",
      options: typeOptions,
      defaultOptionIds: typeOptions.map((o) => o.id),
    },
  ];

  return (
    <div className="flex flex-col gap-4 p-4">
      <FiltersComponent
        className="gap-4"
        filterItems={filters}
        items={[]}
        onFilter={(activeFilters) => {
          onFilterChange(activeFilters);
        }}
        isServerSide={true}
      />
      <div className="text-300">
        <p>
          <strong>Server-side filtering:</strong> Dropdown filters use batch mode. Changes are staged internally and
          only applied when Apply button is clicked.
        </p>
      </div>
    </div>
  );
};

export const ButtonFiltersOnly = () => {
  return (
    <div className="flex flex-col gap-4 p-4">
      <FiltersComponent
        className="gap-4"
        filterItems={buttonFilters}
        items={[]}
        onFilter={(activeFilters) => {
          onFilterChange(activeFilters);
        }}
      />
      <div className="text-300">
        <p>Button filters for quick status-based filtering.</p>
      </div>
    </div>
  );
};

export const DropdownFiltersOnly = () => {
  const filters: FilterItem[] = [
    {
      type: "dropdown",
      title: "Status",
      value: "status",
      options: statusOptions,
      defaultOptionIds: statusOptions.map((o) => o.id),
    },
    {
      type: "dropdown",
      title: "Type",
      value: "type",
      options: typeOptions,
      defaultOptionIds: typeOptions.map((o) => o.id),
    },
  ];

  return (
    <div className="flex flex-col gap-4 p-4">
      <FiltersComponent
        className="gap-4"
        filterItems={filters}
        items={[]}
        onFilter={(activeFilters) => {
          onFilterChange(activeFilters);
        }}
        isServerSide={false}
      />
      <div className="text-300">
        <p>Multiple dropdown filters for multi-dimensional filtering.</p>
      </div>
    </div>
  );
};

export const WithHeaderControls = () => {
  const filters: FilterItem[] = [
    ...buttonFilters.slice(0, 3),
    {
      type: "dropdown",
      title: "Type",
      value: "type",
      options: typeOptions,
      defaultOptionIds: typeOptions.map((o) => o.id),
    },
  ];

  return (
    <div className="flex flex-col gap-4 p-4">
      <FiltersComponent
        className="gap-4"
        filterItems={filters}
        items={[]}
        onFilter={(activeFilters) => {
          onFilterChange(activeFilters);
        }}
        headerControls={<button className="bg-core-primary-100 text-text-inverse rounded-xl px-4 py-2">Export</button>}
      />
      <div className="text-300">
        <p>Filters with additional header controls on the right.</p>
      </div>
    </div>
  );
};

const firmwareOptions = [
  { id: "v3.5.1", label: "v3.5.1" },
  { id: "v3.5.2", label: "v3.5.2" },
  { id: "v3.6.0", label: "v3.6.0" },
];

const zoneOptions = [
  { id: "building-a", label: "Building A" },
  { id: "Austin, Building 1", label: "Austin, Building 1" },
  { id: "remote-site-3", label: "Remote site 3" },
];

export const WithNestedFilterDropdown = () => {
  const statusFilter = {
    type: "dropdown" as const,
    title: "Status",
    value: "status",
    options: statusOptions,
    defaultOptionIds: [],
  };
  const typeFilter = {
    type: "dropdown" as const,
    title: "Type",
    value: "type",
    options: typeOptions,
    defaultOptionIds: [],
  };
  const firmwareFilter = {
    type: "dropdown" as const,
    title: "Firmware",
    value: "firmware",
    options: firmwareOptions,
    defaultOptionIds: [],
  };
  const zoneFilter = {
    type: "dropdown" as const,
    title: "Zones",
    value: "zone",
    options: zoneOptions,
    defaultOptionIds: [],
  };

  const filters: FilterItem[] = [
    // Meta-dropdown rendered first in the bar exposes every category, including
    // firmware + zones which have no standalone trigger.
    {
      type: "nestedFilterDropdown",
      title: "Filters",
      value: "all-filters",
      children: [statusFilter, typeFilter, firmwareFilter, zoneFilter],
    },
    // Standalone shortcuts for the most-used filters. Same `value` keys as the
    // nested children so selecting in either surface stays in sync and pills dedup.
    statusFilter,
    typeFilter,
  ];

  return (
    <div className="flex flex-col gap-4 p-4">
      <FiltersComponent
        className="gap-4"
        filterItems={filters}
        items={[]}
        onFilter={(activeFilters) => {
          onFilterChange(activeFilters);
        }}
        isServerSide
      />
      <div className="text-300">
        <p>
          <strong>Nested dropdown filter:</strong> a meta-dropdown that exposes every category as a hover-triggered
          submenu. Same <code>value</code> keys as standalone triggers, so selecting in either surface stays in sync and
          active-pills dedup. Hover the &ldquo;Filters&rdquo; button, then hover any row to open its submenu.
        </p>
      </div>
    </div>
  );
};

export const StatefulExample = () => {
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>({
    buttonFilters: [defaultListFilter],
    dropdownFilters: {
      status: statusOptions.map((o) => o.id),
      type: typeOptions.map((o) => o.id),
    },

    numericFilters: {},

    textareaListFilters: {},
  });

  const filters: FilterItem[] = [
    ...buttonFilters,
    {
      type: "dropdown",
      title: "Status",
      value: "status",
      options: statusOptions,
      defaultOptionIds: statusOptions.map((o) => o.id),
    },
    {
      type: "dropdown",
      title: "Type",
      value: "type",
      options: typeOptions,
      defaultOptionIds: typeOptions.map((o) => o.id),
    },
  ];

  const handleFilter = (newFilters: ActiveFilters) => {
    setActiveFilters(newFilters);
    onFilterChange(newFilters);
  };

  return (
    <div className="flex flex-col gap-4 p-4">
      <FiltersComponent
        className="gap-4"
        filterItems={filters}
        items={[]}
        onFilter={handleFilter}
        isServerSide={false}
      />
      <div className="bg-surface-secondary rounded-xl p-4 text-300">
        <div className="mb-2 text-heading-200">Current Filter State:</div>
        <div>
          <strong>Button Filters:</strong> {activeFilters.buttonFilters.join(", ")}
        </div>
        <div>
          <strong>Status:</strong>{" "}
          {activeFilters.dropdownFilters.status?.length > 0 ? activeFilters.dropdownFilters.status.join(", ") : "None"}
        </div>
        <div>
          <strong>Type:</strong>{" "}
          {activeFilters.dropdownFilters.type?.length > 0 ? activeFilters.dropdownFilters.type.join(", ") : "None"}
        </div>
      </div>
    </div>
  );
};

export default {
  title: "Shared/List/Filters",
  component: FiltersComponent,
  parameters: {
    docs: {
      description: {
        component:
          "A comprehensive filtering system for list components that supports both button and dropdown filters.\n\n" +
          "**Features:**\n" +
          "- **Button Filters:** Quick single-click filters with status indicators and counts\n" +
          "- **Dropdown Filters:** Multi-select filters with checkboxes and optional Apply/Reset buttons\n" +
          "- **Two Filtering Modes:**\n" +
          "  - **Client-side** (`isServerSide={false}`): Dropdown filters fire callbacks immediately without buttons\n" +
          "  - **Server-side** (`isServerSide={true}`): Dropdown filters show Apply/Reset buttons to reduce API calls\n" +
          "- **Dynamic Button Labels:** Shows filter state (single label, count, or title)\n" +
          "- **Header Controls:** Optional controls that appear on the right side\n" +
          "- **Flexible Configuration:** Mix and match button and dropdown filters\n\n" +
          "**Filter Types:**\n" +
          "- `ButtonFilterItem`: Single-click filters with optional status circle and count\n" +
          "- `DropdownFilterItem`: Multi-select dropdowns with checkbox interface\n\n" +
          "**Implementation Note:**\n" +
          "The `isServerSide` prop determines whether dropdown filters use the `withButtons` option. " +
          "When true, Apply/Reset buttons are shown to batch filter changes into a single API call. " +
          "For client-side filtering, changes apply immediately without buttons.",
      },
    },
  },
  tags: ["autodocs"],
};
