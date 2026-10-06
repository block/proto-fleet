import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import ResponsiveActionGroup, { type ResponsiveActionButton } from ".";
import { sizes, variants } from "@/shared/components/Button";

const actionButtons: ResponsiveActionButton[] = [
  { text: "Reset", variant: variants.secondary, onClick: action("Reset"), testId: "reset" },
  { text: "Duplicate", variant: variants.secondary, onClick: action("Duplicate"), testId: "duplicate" },
  { text: "Delete", variant: variants.secondaryDanger, onClick: action("Delete"), testId: "delete" },
  { text: "Save", variant: variants.primary, onClick: action("Save"), testId: "save" },
];

const ResponsiveActionGroupStory = ({
  buttonSize = sizes.compact,
  primaryButtonStrategy = "primary-or-last",
}: {
  buttonSize?: keyof typeof sizes;
  primaryButtonStrategy?: "primary-or-last" | "last";
}) => (
  <div className="flex min-h-screen items-start justify-end bg-surface-base p-6">
    <ResponsiveActionGroup
      buttons={actionButtons}
      buttonSize={buttonSize}
      primaryButtonStrategy={primaryButtonStrategy}
      triggerTestId="story-more-actions"
    />
  </div>
);

const meta = {
  title: "Shared/ResponsiveActionGroup",
  component: ResponsiveActionGroupStory,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
  args: {
    buttonSize: sizes.compact,
    primaryButtonStrategy: "primary-or-last",
  },
  argTypes: {
    buttonSize: { control: "select", options: Object.values(sizes) },
    primaryButtonStrategy: { control: "select", options: ["primary-or-last", "last"] },
  },
} satisfies Meta<typeof ResponsiveActionGroupStory>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const RightMostPrimary: Story = {
  args: {
    primaryButtonStrategy: "last",
  },
};
