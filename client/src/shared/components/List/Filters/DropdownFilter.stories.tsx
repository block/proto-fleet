import { useState } from "react";
import { action } from "storybook/actions";

import DropdownFilter, { DropdownOption } from "./DropdownFilter";

const onSelect = action("Selection changed");

const statusOptions: DropdownOption[] = [
  { id: "hashing", label: "Hashing" },
  { id: "offline", label: "Offline" },
  { id: "sleeping", label: "Sleeping" },
];

const typeOptions: DropdownOption[] = [
  { id: "proto-rig", label: "Proto Rig" },
  { id: "bitmain", label: "Bitmain" },
  { id: "whatsminer", label: "Whatsminer" },
];

export const WithButtons = () => {
  const [selectedItems, setSelectedItems] = useState<string[]>(["hashing", "offline", "sleeping"]);

  const handleSelect = (items: string[]) => {
    setSelectedItems(items);
    onSelect(items);
  };

  return (
    <div className="flex flex-col gap-4 p-4">
      <DropdownFilter
        title="Status"
        pluralTitle="Statuses"
        options={statusOptions}
        selectedOptions={selectedItems}
        onSelect={handleSelect}
        withButtons={true}
      />
      <div className="text-300">
        <p>
          <strong>With buttons:</strong> Changes are staged internally. Only applied when Apply button is clicked.
          Useful for reducing API calls in server-side filtering.
        </p>
        <p className="mt-2">Selected items: {selectedItems.length > 0 ? selectedItems.join(", ") : "None"}</p>
      </div>
    </div>
  );
};

export const WithoutButtons = () => {
  const [selectedItems, setSelectedItems] = useState<string[]>(["proto-rig"]);

  const handleSelect = (items: string[]) => {
    setSelectedItems(items);
    onSelect(items);
  };

  return (
    <div className="flex flex-col gap-4 p-4">
      <DropdownFilter
        title="Type"
        pluralTitle="Types"
        options={typeOptions}
        selectedOptions={selectedItems}
        onSelect={handleSelect}
        withButtons={false}
      />
      <div className="text-300">
        <p>
          <strong>Without buttons (default):</strong> Changes fire callbacks immediately. Useful for client-side
          filtering where performance is not a concern.
        </p>
        <p className="mt-2">Selected items: {selectedItems.length > 0 ? selectedItems.join(", ") : "None"}</p>
      </div>
    </div>
  );
};

export const ButtonLabelStates = () => {
  return (
    <div className="flex flex-col gap-6 p-4">
      <div>
        <h3 className="mb-2 text-heading-200">One Selected (shows label)</h3>
        <DropdownFilter
          title="Status"
          pluralTitle="Statuses"
          options={statusOptions}
          selectedOptions={["hashing"]}
          onSelect={() => {}}
        />
      </div>

      <div>
        <h3 className="mb-2 text-heading-200">Multiple Selected (shows count)</h3>
        <DropdownFilter
          title="Status"
          pluralTitle="Statuses"
          options={statusOptions}
          selectedOptions={["hashing", "offline"]}
          onSelect={() => {}}
        />
      </div>

      <div>
        <h3 className="mb-2 text-heading-200">All Selected (shows title)</h3>
        <DropdownFilter
          title="Status"
          pluralTitle="Statuses"
          options={statusOptions}
          selectedOptions={["hashing", "offline", "sleeping"]}
          onSelect={() => {}}
        />
      </div>

      <div>
        <h3 className="mb-2 text-heading-200">None Selected (shows title)</h3>
        <DropdownFilter
          title="Status"
          pluralTitle="Statuses"
          options={statusOptions}
          selectedOptions={[]}
          onSelect={() => {}}
        />
      </div>
    </div>
  );
};

export const MultipleFilters = () => {
  const [statusSelected, setStatusSelected] = useState<string[]>(["hashing"]);
  const [typeSelected, setTypeSelected] = useState<string[]>(["proto-rig"]);

  return (
    <div className="flex flex-col gap-6 p-4">
      <div className="flex gap-4">
        <DropdownFilter
          title="Status"
          pluralTitle="Statuses"
          options={statusOptions}
          selectedOptions={statusSelected}
          onSelect={(items) => {
            setStatusSelected(items);
            onSelect(items);
          }}
          withButtons={true}
        />
        <DropdownFilter
          title="Type"
          pluralTitle="Types"
          options={typeOptions}
          selectedOptions={typeSelected}
          onSelect={(items) => {
            setTypeSelected(items);
            onSelect(items);
          }}
          withButtons={true}
        />
      </div>
      <div className="text-300">
        <p>
          <strong>Multiple filters with buttons:</strong> Each filter can be configured independently.
        </p>
        <div className="mt-2">
          <div>Status: {statusSelected.length > 0 ? statusSelected.join(", ") : "None"}</div>
          <div>Type: {typeSelected.length > 0 ? typeSelected.join(", ") : "None"}</div>
        </div>
      </div>
    </div>
  );
};

const manyGroupOptions: DropdownOption[] = Array.from({ length: 20 }, (_, i) => ({
  id: `group-${i + 1}`,
  label: `Group ${i + 1}`,
}));

export const ManyOptions = () => {
  const [selectedItems, setSelectedItems] = useState<string[]>(["group-1", "group-5"]);

  const handleSelect = (items: string[]) => {
    setSelectedItems(items);
    onSelect(items);
  };

  return (
    <div className="flex flex-col gap-4 p-4">
      <DropdownFilter
        title="Groups"
        options={manyGroupOptions}
        selectedOptions={selectedItems}
        onSelect={handleSelect}
        withButtons={true}
      />
      <div className="text-300">
        <p>
          <strong>Many options (20):</strong> The dropdown scrolls when the list exceeds the max height. Apply/Reset
          buttons stay fixed at the bottom.
        </p>
        <p className="mt-2">Selected items: {selectedItems.length > 0 ? selectedItems.join(", ") : "None"}</p>
      </div>
    </div>
  );
};

export const ManyOptionsWithoutButtons = () => {
  const [selectedItems, setSelectedItems] = useState<string[]>(["group-3"]);

  const handleSelect = (items: string[]) => {
    setSelectedItems(items);
    onSelect(items);
  };

  return (
    <div className="flex flex-col gap-4 p-4">
      <DropdownFilter
        title="Groups"
        options={manyGroupOptions}
        selectedOptions={selectedItems}
        onSelect={handleSelect}
        withButtons={false}
      />
      <div className="text-300">
        <p>
          <strong>Many options without buttons (20):</strong> Same scrollable list but without Apply/Reset, giving more
          visible space for options. Changes apply immediately on click.
        </p>
        <p className="mt-2">Selected items: {selectedItems.length > 0 ? selectedItems.join(", ") : "None"}</p>
      </div>
    </div>
  );
};

export default {
  title: "Shared/List/Filters/DropdownFilter",
  component: DropdownFilter,
  parameters: {
    docs: {
      description: {
        component:
          "DropdownFilter component for filtering list items with a dropdown interface.\n\n" +
          "**API Design:**\n" +
          "- Single `onSelect` callback that receives the full array of selected items\n" +
          "- `withButtons` boolean controls whether to show Apply/Reset buttons\n" +
          "- With buttons: Changes are staged internally and applied on Apply click\n" +
          "- Without buttons (default): Callbacks fire immediately on every change\n\n" +
          "**Features:**\n" +
          "- Two-tone button design with grey background when filters are active\n" +
          "- Dynamic button labels showing filter state\n" +
          "- Select all with partial selection support\n" +
          "- Rotating chevron icon\n" +
          "- Implementer decides whether to use buttons based on performance needs",
      },
    },
  },
  tags: ["autodocs"],
};
