import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import NoFansDetectedDialog from "./NoFansDetectedDialog";

const meta: Meta<typeof NoFansDetectedDialog> = {
  title: "Proto OS/Onboarding/NoFansDetectedDialog",
  component: NoFansDetectedDialog,
  parameters: {
    layout: "centered",
  },
  args: {
    onUseAirCooling: action("onUseAirCooling"),
    onConfirmImmersionCooling: action("onConfirmImmersionCooling"),
  },
};

export default meta;
type Story = StoryObj<typeof NoFansDetectedDialog>;

export const Default: Story = {};

export const Loading: Story = {
  args: {
    loading: true,
  },
};
