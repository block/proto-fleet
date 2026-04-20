import type { Meta, StoryObj } from "@storybook/react";
import EmptySlotCard from "./EmptySlotCard";

const meta: Meta<typeof EmptySlotCard> = {
  title: "ProtoOS/Diagnostic/EmptySlotCard",
  component: EmptySlotCard,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    type: {
      control: "select",
      options: ["fan", "hashboard", "psu"],
      description: "Type of component slot",
    },
    position: {
      control: "number",
      description: "Position/slot number for the component",
    },
    title: {
      control: "text",
      description: "Title to display in the card header",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const EmptyFan: Story = {
  args: {
    type: "fan",
    position: 1,
    title: "Fan 1",
  },
};

export const EmptyHashboard: Story = {
  args: {
    type: "hashboard",
    position: 1,
    title: "Hashboard 1",
  },
};

export const EmptyPsu: Story = {
  args: {
    type: "psu",
    position: 1,
    title: "PSU 1",
  },
};

export const EmptyFanPosition4: Story = {
  args: {
    type: "fan",
    position: 4,
    title: "Fan 4",
  },
};

export const EmptyHashboardPosition3: Story = {
  args: {
    type: "hashboard",
    position: 3,
    title: "Hashboard 3",
  },
};
