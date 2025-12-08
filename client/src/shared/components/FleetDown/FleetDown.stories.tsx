import type { Meta, StoryObj } from "@storybook/react";

import FleetDown from "./FleetDown";

const meta = {
  title: "Proto Fleet/FleetDown",
  component: FleetDown,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof FleetDown>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  parameters: {
    docs: {
      description: {
        story: "Error page displayed when the backend server is completely down.",
      },
    },
  },
};
