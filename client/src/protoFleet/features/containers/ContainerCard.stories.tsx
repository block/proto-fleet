import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import ContainerCard from "./ContainerCard";
import { ContainerSchema, TankHealthSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

const sample = create(ContainerSchema, {
  deviceIdentifier: "ct1-demo",
  embeddedWebViewAvailable: true,
  name: "Container 1",
  site: { id: 1n, label: "West site" },
  hashrateHs: 38e15,
  powerW: 920000,
  tanks: [0, 1, 2, 3, 6, 8].map((affected, index) =>
    create(TankHealthSchema, { position: index + 1, hashingCount: 16 - affected, attentionCount: affected }),
  ),
});
const meta = {
  title: "Proto Fleet/Containers/ContainerCard",
  component: ContainerCard,
  decorators: [
    (Story) => (
      <div className="w-80 p-3">
        <Story />
      </div>
    ),
  ],
  args: { container: sample },
} satisfies Meta<typeof ContainerCard>;
export default meta;
type Story = StoryObj<typeof meta>;
export const TankHealth: Story = {};
export const Stale: Story = { args: { container: create(ContainerSchema, { ...sample, stale: true }) } };
export const Incomplete: Story = {
  args: {
    container: create(ContainerSchema, {
      ...sample,
      tanks: [create(TankHealthSchema, { position: 1, hashingCount: 15, unknownCount: 1 })],
    }),
  },
};

export const FleetNode: Story = {
  args: {
    container: create(ContainerSchema, { ...sample, embeddedWebViewAvailable: false, url: "http://192.168.1.20:8080" }),
  },
};
export const UnavailableView: Story = {
  args: { container: create(ContainerSchema, { ...sample, embeddedWebViewAvailable: false }) },
};
