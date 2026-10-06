import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { defaultBehavior } from "./behaviorUtils";
import { getChannelSettingsChanges } from "./channelSettingsChangesUtils";
import MinerScopeChangesModal from "./MinerScopeChangesModal";
import { canaryChannel } from "./ReleaseChannels.fixtures";
import { ReleaseChannelScopeSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const identifiers = Array.from({ length: 125 }, (_, index) => `rig-${String(index + 1).padStart(3, "0")}`);
const original = { ...canaryChannel, behavior: defaultBehavior() };
const changes = getChannelSettingsChanges(
  { ...original, scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: identifiers.slice(0, 100) }) },
  { ...original, scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: identifiers.slice(25) }) },
  Object.fromEntries(identifiers.map((identifier, index) => [identifier, `Rig ${index + 1}`])),
).minerChanges!;

const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Miner Changes",
  component: MinerScopeChangesModal,
  args: { changes, onClose: () => {} },
} satisfies Meta<typeof MinerScopeChangesModal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const LargeSelection: Story = {};
