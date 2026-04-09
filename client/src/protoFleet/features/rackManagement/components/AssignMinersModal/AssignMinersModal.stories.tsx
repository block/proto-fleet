import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { DismissCircle } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

const sampleMiners = [
  { id: "m1", name: "Miner-001", ip: "192.168.1.10", model: "S19 Pro" },
  { id: "m2", name: "Miner-002", ip: "192.168.1.11", model: "S19 Pro" },
  { id: "m3", name: "Miner-003", ip: "192.168.1.12", model: "S19j Pro" },
  { id: "m4", name: "Miner-004", ip: "192.168.1.13", model: "S19j Pro" },
  { id: "m5", name: "Miner-005", ip: "192.168.1.14", model: "S19 XP" },
];

const MockMinersPane = () => (
  <div className="flex h-full flex-col gap-3 p-4">
    <div className="flex items-center justify-between">
      <div className="text-emphasis-300 text-text-primary">Miners ({sampleMiners.length})</div>
      <button className="text-core-primary text-200">Clear all</button>
    </div>
    <div className="flex gap-2">
      {["Manual", "By Name", "By Network"].map((mode) => (
        <button
          key={mode}
          className="first:text-core-primary rounded-lg bg-surface-5 px-3 py-1.5 text-200 text-text-primary first:bg-core-primary-10"
        >
          {mode}
        </button>
      ))}
    </div>
    <div className="flex flex-col gap-1">
      {sampleMiners.map((miner) => (
        <div key={miner.id} className="flex items-center justify-between rounded-lg px-3 py-2 hover:bg-surface-5">
          <div className="flex flex-col">
            <span className="text-300 text-text-primary">{miner.name}</span>
            <span className="text-200 text-text-primary-50">
              {miner.ip} &middot; {miner.model}
            </span>
          </div>
        </div>
      ))}
    </div>
  </div>
);

const MockRackPane = ({ rows, cols }: { rows: number; cols: number }) => (
  <div className="flex h-full flex-col gap-3">
    <div className="flex items-center justify-between">
      <div className="text-emphasis-300 text-text-primary">Rack Grid</div>
      <div className="text-200 text-text-primary-50">
        {sampleMiners.length}/{rows * cols} assigned
      </div>
    </div>
    <div className="grid gap-1" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
      {Array.from({ length: rows * cols }, (_, i) => {
        const isAssigned = i < sampleMiners.length;
        return (
          <div
            key={i}
            className={`flex aspect-square items-center justify-center rounded-lg text-200 ${
              isAssigned ? "text-core-primary bg-core-primary-10" : "bg-surface-base text-text-primary-30"
            }`}
          >
            {i + 1}
          </div>
        );
      })}
    </div>
  </div>
);

type AssignMinersModalStoryProps = {
  infoMessage: string;
  isLoading?: boolean;
  showError?: boolean;
  rackLabel?: string;
  rows?: number;
  cols?: number;
};

const AssignMinersModalStory = ({
  infoMessage,
  isLoading = false,
  showError = false,
  rackLabel = "Rack A-01",
  rows = 4,
  cols = 5,
}: AssignMinersModalStoryProps) => {
  const [open, setOpen] = useState(true);

  if (!open) {
    return (
      <div className="flex h-screen items-center justify-center bg-surface-base">
        <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-surface-base p-4">
      <div className="mb-4 max-w-3xl rounded-lg bg-intent-info-10 p-4 text-300 text-text-primary">{infoMessage}</div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <FullScreenTwoPaneModal
        open
        title={rackLabel}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        buttons={[
          {
            text: "Delete Rack",
            variant: variants.secondaryDanger,
            onClick: action("deleteRack"),
          },
          {
            text: "Edit Rack Settings",
            variant: variants.secondary,
            onClick: action("editRackSettings"),
          },
          {
            text: "Manage Miners",
            variant: variants.secondary,
            onClick: action("manageMiners"),
          },
          {
            text: "Save",
            variant: variants.primary,
            onClick: action("save"),
          },
        ]}
        abovePanes={
          showError ? (
            <div className="shrink-0 px-2 pb-4">
              <Callout
                intent="danger"
                prefixIcon={<DismissCircle />}
                title="Cannot add 25 miners with only 20 available slots. Deselect some miners or update your rack settings."
                dismissible
              />
            </div>
          ) : undefined
        }
        loadingState={
          isLoading ? (
            <div className="flex flex-1 items-center justify-center">
              <ProgressCircular indeterminate />
            </div>
          ) : undefined
        }
        primaryPane={<MockMinersPane />}
        secondaryPane={
          <div className="p-4">
            <MockRackPane rows={rows} cols={cols} />
          </div>
        }
      />
    </div>
  );
};

const meta = {
  title: "Proto Fleet/Rack Management/AssignMinersModal",
  component: AssignMinersModalStory,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof AssignMinersModalStory>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    infoMessage: "Assign miners modal with mock miner list and rack grid. Uses full-width layout (no max-width).",
  },
};

export const Loading: Story = {
  args: {
    infoMessage: "Assign miners modal in loading state while fetching existing rack data.",
    isLoading: true,
  },
};

export const WithError: Story = {
  args: {
    infoMessage: "Assign miners modal showing an error callout above the panes.",
    showError: true,
  },
};

export const LargeRack: Story = {
  args: {
    infoMessage: "Assign miners modal with a larger rack configuration (6 rows x 8 columns).",
    rackLabel: "Rack B-03",
    rows: 6,
    cols: 8,
  },
};
