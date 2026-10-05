import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import ContainerControls, { type ContainerToggleControl } from "./ContainerControls";

const initialControls: ContainerToggleControl[] = [
  { id: "ac-1", label: "AC 1", metric: "75°F", on: true },
  { id: "ac-2", label: "AC 2", metric: "75°F", on: true },
  { id: "cdu-fans", label: "CDU cooling fans", metric: "60% speed", on: true },
  { id: "coolant-pump", label: "Coolant pump", metric: "50 Hz", on: true },
  { id: "dry_cooler", label: "Dry cooler", metric: "118°F, 50% speed", on: true },
  { id: "dry_cooler_auto", label: "Dry cooler auto", on: true },
  { id: "tank-a-light", label: "Tank A light", on: true },
  { id: "tank-b-light", label: "Tank B light", on: true },
  { id: "logo-light", label: "Logo light", on: true },
];

const InteractiveContainerControls = () => {
  const [controls, setControls] = useState(initialControls);

  const onToggle = (id: string, on: boolean) =>
    setControls((current) => current.map((item) => (item.id === id ? { ...item, on } : item)));

  return (
    <div className="flex flex-col gap-6 bg-surface-base p-8">
      <ContainerControls
        kind="cooling"
        controls={controls.filter((control) => control.id === "dry_cooler" || control.id === "dry_cooler_auto")}
        onToggle={onToggle}
      />
      <ContainerControls
        controls={controls.filter((control) => control.id !== "dry_cooler" && control.id !== "dry_cooler_auto")}
        onToggle={onToggle}
        alarm={{ label: "Alarm", onReset: () => undefined, onMute: () => undefined }}
      />
    </div>
  );
};

const meta = {
  title: "Proto Fleet/Containers/Container Controls",
  component: InteractiveContainerControls,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Container-level auxiliary controls in a compact responsive four-column grid. Toggle state is owned by the caller; Reset and Mute are explicit command callbacks. The prominent metric line stays textual so radial gauges remain reserved for fans.",
      },
    },
  },
} satisfies Meta<typeof InteractiveContainerControls>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const CommandPending: Story = {
  render: () => (
    <ContainerControls
      controls={[
        {
          id: "dry_cooler",
          label: "Dry cooler",
          on: false,
          disabled: true,
          pending: true,
        },
        { id: "coolant_pump", label: "Coolant pump", on: undefined, stateLabel: "Unknown" },
        { id: "main_circuit_breaker", label: "Main circuit breaker", on: true, disabled: true, stateLabel: "Closed" },
      ]}
      onToggle={() => undefined}
    />
  ),
};
