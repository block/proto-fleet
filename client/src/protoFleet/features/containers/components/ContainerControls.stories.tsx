import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import ContainerControls, { type ContainerToggleControl } from "./ContainerControls";

const initialControls: ContainerToggleControl[] = [
  { id: "ac-1", label: "AC 1", metric: "75°F", icon: "fan", on: true },
  { id: "ac-2", label: "AC 2", metric: "75°F", icon: "fan", on: true },
  { id: "cdu-fans", label: "CDU cooling fans", metric: "60% speed", icon: "fan", on: true },
  { id: "coolant-pump", label: "Coolant pump", metric: "50 Hz", icon: "pump", on: true },
  { id: "dry-cooler", label: "Dry cooler", metric: "118°F, 50% speed", icon: "thermometer", on: true },
  { id: "dry-cooler-auto", label: "Dry cooler auto", icon: "thermometer", on: true },
  { id: "tank-a-light", label: "Tank A light", icon: "light", on: true },
  { id: "tank-b-light", label: "Tank B light", icon: "light", on: true },
  { id: "logo-light", label: "Logo light", icon: "light", on: true },
];

const InteractiveContainerControls = () => {
  const [controls, setControls] = useState(initialControls);

  return (
    <div className="bg-surface-base p-8">
      <ContainerControls
        controls={controls}
        onToggle={(id, on) =>
          setControls((current) => current.map((item) => (item.id === id ? { ...item, on } : item)))
        }
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
          "Container-level auxiliary controls in a responsive two-column grid. Toggle state is owned by the caller; Reset and Mute are explicit command callbacks. The prominent metric line stays textual so radial gauges remain reserved for fans.",
      },
    },
  },
} satisfies Meta<typeof InteractiveContainerControls>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
