import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import CardHeader from "./CardHeader";
import { Alert, Fan, Settings, Success } from "@/shared/assets/icons";

const meta: Meta<typeof CardHeader> = {
  title: "Proto OS/Diagnostic/CardHeader",
  component: CardHeader,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    title: {
      control: "text",
      description: "Header title",
    },
    statusIcon: {
      control: false,
      description: "Optional status icon element",
    },
    componentIcon: {
      control: false,
      description: "Optional component icon element",
    },
    onInfoIconClick: {
      action: "onInfoIconClick",
      description: "Info icon click handler",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    title: "Fans",
    statusIcon: <Success className="text-intent-success-fill" />,
    componentIcon: <Fan className="text-text-primary-70" />,
    onInfoIconClick: action("onInfoIconClick"),
  },
};

export const WarningStatus: Story = {
  args: {
    title: "Fans",
    statusIcon: <Alert className="text-intent-critical-fill" />,
    componentIcon: <Fan className="text-text-primary-70" />,
    onInfoIconClick: action("onInfoIconClick"),
  },
};

export const WithoutStatusIcon: Story = {
  args: {
    title: "Settings",
    statusIcon: null,
    componentIcon: <Settings className="text-text-primary-70" />,
    onInfoIconClick: action("onInfoIconClick"),
  },
};

export const WithoutComponentIcon: Story = {
  args: {
    title: "System Status",
    statusIcon: <Success className="text-intent-success-fill" />,
    componentIcon: null,
    onInfoIconClick: action("onInfoIconClick"),
  },
};

export const LongTitle: Story = {
  args: {
    title: "Cooling Subsystem – Intake/Exhaust Fan Performance Overview",
    statusIcon: <Success className="text-intent-success-fill" />,
    componentIcon: <Fan className="text-text-primary-70" />,
    onInfoIconClick: action("onInfoIconClick"),
  },
};
