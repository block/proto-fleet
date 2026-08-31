import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import FirmwarePickerButton, { type FirmwareOption } from "./FirmwarePickerButton";

// The button-styled firmware selector from the model group row: the trigger
// shows the selected version and opens a listbox of the versions targeting
// the model. Selection state is interactive in these stories.
const meta = {
  title: "Proto Fleet/Firmware/Rollout Channels/Firmware Picker",
  component: FirmwarePickerButton,
  parameters: {
    layout: "centered",
  },
} satisfies Meta<typeof FirmwarePickerButton>;

export default meta;

type Story = StoryObj<typeof FirmwarePickerButton>;

const rigOptions: FirmwareOption[] = [
  { value: "", label: "No firmware" },
  { value: "fw-rig-143", label: "1.4.3", description: "proto-rig-1.4.3.swu" },
  { value: "fw-rig-144", label: "1.4.4", description: "proto-rig-1.4.4.swu" },
];

const InteractivePicker = ({ initialValue }: { initialValue: string }) => {
  const [value, setValue] = useState(initialValue);
  return (
    <FirmwarePickerButton
      label="Firmware for Rig"
      options={rigOptions}
      value={value}
      onChange={setValue}
      testId="story-firmware-picker"
    />
  );
};

export const VersionSelected: Story = {
  name: "Version selected",
  render: () => <InteractivePicker initialValue="fw-rig-144" />,
};

export const NoFirmwareAssigned: Story = {
  name: "No firmware assigned",
  render: () => <InteractivePicker initialValue="" />,
};
