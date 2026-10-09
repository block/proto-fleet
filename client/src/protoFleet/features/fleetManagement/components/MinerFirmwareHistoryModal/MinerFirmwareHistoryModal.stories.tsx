import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";

import { MinerFirmwareHistoryModalView } from "./MinerFirmwareHistoryModal";
import { historyEntries } from "./MinerFirmwareHistoryModal.fixtures";

const history = {
  entries: historyEntries,
  canRead: true,
  hasLoaded: true,
  isLoading: false,
  isLoadingMore: false,
  hasMore: true,
  error: null,
  refresh: fn(),
  loadMore: fn(),
  retry: fn(),
};

const meta = {
  title: "Proto Fleet/Fleet Management/Miner Firmware History",
  component: MinerFirmwareHistoryModalView,
  parameters: { layout: "fullscreen" },
  argTypes: { history: { control: false }, onViewUpdate: { control: false } },
  args: {
    deviceIdentifier: "proto-rig-123",
    minerName: "Rack 12 · Miner 3",
    history,
    onClose: fn(),
    onViewUpdate: fn(),
  },
} satisfies Meta<typeof MinerFirmwareHistoryModalView>;
export default meta;
type Story = StoryObj<typeof meta>;

export const History: Story = {};
export const Loading: Story = {
  args: { history: { ...history, entries: [], hasLoaded: false, isLoading: true, hasMore: false } },
};
export const Empty: Story = { args: { history: { ...history, entries: [], hasMore: false } } };
export const PageError: Story = {
  args: { history: { ...history, error: "The request timed out. Try again." } },
};
export const Unavailable: Story = {
  args: { history: { ...history, entries: [], canRead: false, hasMore: false } },
};
