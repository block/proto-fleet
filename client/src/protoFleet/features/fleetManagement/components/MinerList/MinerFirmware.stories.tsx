import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";
import MinerFirmware from "./MinerFirmware";
import {
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

const miner = create(MinerStateSnapshotSchema, {
  deviceIdentifier: "proto-rig-123",
  name: "Rack 12 · Miner 3",
  firmwareVersion: "1.4.3",
  deviceStatus: DeviceStatus.ONLINE,
  pairingStatus: PairingStatus.PAIRED,
});

const meta = {
  title: "Proto Fleet/Fleet Management/Miner Firmware",
  component: MinerFirmware,
  parameters: { layout: "centered" },
  decorators: [
    (Story) => (
      <div className="w-[120px]">
        <Story />
      </div>
    ),
  ],
  args: { miner, onViewHistory: fn() },
  argTypes: { onViewHistory: { control: false } },
} satisfies Meta<typeof MinerFirmware>;
export default meta;
type Story = StoryObj<typeof meta>;

export const KnownVersion: Story = {};

export const UnknownVersion: Story = {
  args: {
    miner: create(MinerStateSnapshotSchema, {
      ...miner,
      firmwareVersion: "",
      deviceStatus: DeviceStatus.OFFLINE,
      pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
    }),
  },
};

export const LongVersion: Story = {
  args: {
    miner: create(MinerStateSnapshotSchema, {
      ...miner,
      firmwareVersion: "v1.4.3-release-candidate.12+20260926",
    }),
  },
};

export const ReadOnly: Story = { args: { onViewHistory: undefined } };
