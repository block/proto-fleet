import type { Meta, StoryObj } from "@storybook/react-vite";
import SettingsPageHeader from "./SettingsPageHeader";

const meta = {
  title: "Proto Fleet/Settings/SettingsPageHeader",
  component: SettingsPageHeader,
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
    title: "Team",
    description: "Define what members can see and do. Assign roles when you add or edit a member.",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof SettingsPageHeader>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WithDescription: Story = {};

export const TitleOnly: Story = {
  args: {
    title: "Firmware",
    description: undefined,
  },
};
