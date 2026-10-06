import type { Meta, StoryObj } from "@storybook/react-vite";
import { action } from "storybook/actions";

import FirmwareUsageBadge from "./FirmwareUsageBadge";
import { canaryChannel, checksums, productionChannel } from "./ReleaseChannels/ReleaseChannels.fixtures";

const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Firmware usage",
  component: FirmwareUsageBadge,
  args: {
    checksum: checksums.rig144,
    filename: "proto-rig-1.4.4.swu",
    api: { channels: [canaryChannel], rollouts: [], hasLoaded: true, error: null },
    refreshWarning: null,
    onManageChannel: action("Manage channel"),
  },
  decorators: [
    (Story) => (
      <div className="p-6">
        <p className="mb-4 text-300 text-text-primary-50">Version</p>
        <div className="flex w-36 flex-wrap items-center gap-2 text-300 text-text-primary">
          <span>1.4.4</span>
          <Story />
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof FirmwareUsageBadge>;

export default meta;
type Story = StoryObj<typeof meta>;

export const InUse: Story = {};

export const MultipleChannels: Story = {
  args: {
    api: {
      ...meta.args.api,
      channels: [canaryChannel, { ...productionChannel, modelGroups: canaryChannel.modelGroups }],
    },
  },
};
