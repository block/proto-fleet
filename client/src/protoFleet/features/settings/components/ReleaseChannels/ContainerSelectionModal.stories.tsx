import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ContainerSelectionModal from "./ContainerSelectionModal";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

const containers = [
  create(ContainerSchema, { deviceIdentifier: "ct1-north-a", name: "North A", site: { id: 1n, label: "North" } }),
  create(ContainerSchema, { deviceIdentifier: "ct1-north-b", name: "North B", site: { id: 1n, label: "North" } }),
  create(ContainerSchema, { deviceIdentifier: "ct1-spare", name: "Container SN-0042" }),
];

const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Container Selection",
  component: ContainerSelectionModal,
  args: {
    containers,
    loading: false,
    hasLoadError: false,
    selectedIdentifiers: ["ct1-north-a"],
    onDismiss: () => {},
    onSave: () => {},
  },
} satisfies Meta<typeof ContainerSelectionModal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Selection: Story = {};
export const NoContainers: Story = { args: { containers: [], selectedIdentifiers: [] } };
export const LoadError: Story = { args: { containers: [], hasLoadError: true } };
