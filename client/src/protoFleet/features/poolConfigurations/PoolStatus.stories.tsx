import type { Meta, StoryObj } from "@storybook/react";
import { fn } from "storybook/test";
import { PoolStatus } from "./PoolStatus";

const meta = {
  title: "Proto Fleet/Pools/Pool status",
  component: PoolStatus,
  parameters: { layout: "centered" },
  args: { status: "Verified", name: "Production", onDetails: fn() },
} satisfies Meta<typeof PoolStatus>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Verified: Story = {};
export const NeedsAttention: Story = { args: { status: "Update failed" } };
export const DeviceConfiguration: Story = { args: { status: "Device configuration" } };
export const Updating: Story = { args: { status: "Updating…" } };
