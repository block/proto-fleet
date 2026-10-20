import type { Meta, StoryObj } from "@storybook/react-vite";
import SitesSection from "./SitesSection";

const meta = {
  title: "Proto Fleet/Dashboard/SitesSection",
  component: SitesSection,
  decorators: [
    (Story) => (
      <div className="bg-surface-base py-6 text-text-primary">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof SitesSection>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = { args: { sites: [] } };
export const Loading: Story = { args: { sites: undefined } };
