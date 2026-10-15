import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";
import NodeDetailsModal from "./NodeDetailsModal";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";

const meta = {
  title: "Proto Fleet/Settings/Node details",
  component: NodeDetailsModal,
  args: {
    node: {
      fleetNodeId: "7",
      pendingEnrollmentId: null,
      name: "fleet-pi-3",
      enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
      identityFingerprint: "21efefb471875859",
      commandProtocolUpgradeRequired: false,
      commandProtocolUpgradeRecommended: false,
      controlStreamConnected: true,
      pairedDeviceCount: 128,
      createdAt: new Date("2026-10-01T12:00:00Z"),
      lastSeenAt: new Date(),
      ipAddresses: ["192.168.1.7", "fd00:1234:5678:9012:abcd:1234:5678:9012"],
      version: "v0.5.1-internal.3",
      enrolledBy: "operator@example.com",
      enrolledAt: new Date("2026-10-01T12:05:00Z"),
    },
    canFindMiners: true,
    onDismiss: fn(),
    onFindMiners: fn(),
  },
} satisfies Meta<typeof NodeDetailsModal>;

export default meta;
type Story = StoryObj<typeof meta>;
export const Connected: Story = {};
export const Unavailable: Story = {
  args: {
    node: {
      ...meta.args.node,
      ipAddresses: [],
      version: "",
      enrolledBy: "",
      enrolledAt: null,
      lastSeenAt: null,
      controlStreamConnected: false,
    },
  },
};
