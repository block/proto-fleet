import type { Meta, StoryObj } from "@storybook/react";
import { expect, userEvent, within } from "storybook/test";
import MarketDataInfoPopover from "./MarketDataInfoPopover";

const meta = {
  title: "Proto Fleet/Dashboard/Market data info",
  component: MarketDataInfoPopover,
  tags: ["autodocs"],
  decorators: [
    (Story) => (
      <div className="min-h-96 p-6">
        <div className="flex items-center gap-2">
          <span className="text-emphasis-400 text-text-primary">Market data</span>
          <Story />
        </div>
      </div>
    ),
  ],
  parameters: {
    docs: {
      description: {
        component:
          "Click the info icon for estimation and freshness details. The shared popover becomes a bottom sheet on phone-width viewports. Dismiss with the icon, an outside click, or Escape.",
      },
    },
  },
} satisfies Meta<typeof MarketDataInfoPopover>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Closed: Story = {};

export const Open: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const page = within(canvasElement.ownerDocument.body);
    const trigger = canvas.getByRole("button", { name: "About market data" });
    await userEvent.click(trigger);
    await expect(trigger).toHaveAttribute("aria-expanded", "true");
    await expect(await page.findByRole("region", { name: "About market data" })).toBeVisible();
  },
};

export const DarkOpen: Story = {
  globals: { theme: "dark" },
  play: Open.play,
};

export const KeyboardDismiss: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const page = within(canvasElement.ownerDocument.body);
    const trigger = canvas.getByRole("button", { name: "About market data" });
    trigger.focus();
    await userEvent.keyboard("{Enter}");
    await expect(await page.findByRole("region", { name: "About market data" })).toBeVisible();
    await userEvent.keyboard("{Escape}");
    await expect(page.queryByRole("region", { name: "About market data" })).not.toBeInTheDocument();
    await expect(trigger).toHaveAttribute("aria-expanded", "false");
    await expect(trigger).toHaveFocus();
  },
};
