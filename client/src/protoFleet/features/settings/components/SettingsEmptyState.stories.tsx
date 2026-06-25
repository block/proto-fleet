import type { Meta, StoryObj } from "@storybook/react-vite";
import SettingsEmptyState from "./SettingsEmptyState";

const meta = {
  title: "Proto Fleet/Settings/SettingsEmptyState",
  component: SettingsEmptyState,
  parameters: {
    layout: "centered",
  },
  decorators: [
    (Story) => (
      <div className="w-[720px] bg-surface-base">
        <Story />
      </div>
    ),
  ],
  args: {
    title: "No firmware files uploaded",
    description: "Upload firmware before deploying updates to your fleet.",
  },
  argTypes: {
    size: {
      control: "select",
      options: ["default", "section"],
    },
  },
  tags: ["autodocs"],
} satisfies Meta<typeof SettingsEmptyState>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Section: Story = {
  args: {
    title: "No schedules yet",
    description: "Configure schedules to automate actions for your miners.",
    size: "section",
  },
};
