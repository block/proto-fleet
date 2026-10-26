import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";
import NodeDetailsModal from "./NodeDetailsModal";
import {
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";

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
      haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
      haPeerNodeId: null,
      haPeerNodeName: "",
      haStandbyReady: false,
      haStatusReason: "",
      createdAt: new Date("2026-10-01T12:00:00Z"),
      lastSeenAt: new Date(),
      ipAddresses: ["192.168.1.7", "fd00:1234:5678:9012:abcd:1234:5678:9012"],
      version: "v0.5.1-internal.3",
      enrolledBy: "operator@example.com",
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
      lastSeenAt: null,
      controlStreamConnected: false,
    },
  },
};

export const Active: Story = {
  args: {
    node: {
      ...meta.args.node,
      haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE,
      haPeerNodeId: "8",
      haPeerNodeName: "node-01",
      haStandbyReady: true,
    },
  },
};
export const Standby: Story = {
  args: {
    node: {
      ...meta.args.node,
      haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
      haPeerNodeId: "3",
      haPeerNodeName: "node-00",
      haStandbyReady: true,
    },
  },
};
export const PreparingStandby: Story = {
  args: {
    node: {
      ...meta.args.node,
      haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
      haPeerNodeId: "3",
      haPeerNodeName: "node-00",
      haStandbyReady: false,
      haStatusReason: "Waiting for both nodes to connect",
    },
  },
};
