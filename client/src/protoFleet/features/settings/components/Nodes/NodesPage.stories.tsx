import { type ReactNode, useLayoutEffect } from "react";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { Meta, StoryObj } from "@storybook/react-vite";

import NodesPage from "./NodesPage";
import { fleetNodeAdminClient } from "@/protoFleet/api/clients";
import {
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
  type FleetNodeSummary,
  FleetNodeSummarySchema,
  ListFleetNodesResponseSchema,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { useFleetStore } from "@/protoFleet/store";

type MutableClient<T> = { -readonly [K in keyof T]: T[K] };
type MessageOverrides<T> = Omit<Partial<T>, "$typeName" | "$unknown">;

const mutableFleetNodeAdminClient = fleetNodeAdminClient as MutableClient<typeof fleetNodeAdminClient>;

const STORYBOOK_OPERATOR = "storybook-operator";
const NODE_PERMISSIONS = ["fleetnode:read", "fleetnode:manage"];

const secondsAgo = (seconds: number) => timestampFromDate(new Date(Date.now() - seconds * 1_000));

const node = (overrides: MessageOverrides<FleetNodeSummary>) =>
  create(FleetNodeSummarySchema, {
    enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
    identityFingerprint: "21efefb471875859",
    controlStreamConnected: true,
    discoveryConfigReported: true,
    createdAt: timestampFromDate(new Date("2026-10-01T12:00:00Z")),
    lastSeenAt: secondsAgo(5),
    version: "v0.6.2-internal.13",
    enrolledBy: "operator@example.com",
    ...overrides,
  });

// Built per fetch so heartbeats stay fresh while the story polls.
const allStates = () => [
  node({
    fleetNodeId: 1n,
    name: "stl-lab-proto-fleet-rack-1",
    ipAddresses: ["172.16.21.152"],
    discoveryRanges: ["172.16.21.0-172.16.21.255"],
    pairedDeviceCount: 18n,
  }),
  node({
    fleetNodeId: 2n,
    name: "mar-lab-proto-fleet-rack-2",
    ipAddresses: ["172.16.2.66", "172.16.2.67", "10.0.0.4", "fd00:1234:5678:9012:abcd:1234:5678:9012"],
    discoveryRanges: ["172.16.2.0-172.16.2.255"],
    pairedDeviceCount: 15n,
    version: "v0.6.2-internal.8",
    commandProtocolUpgradeRecommended: true,
  }),
  node({
    fleetNodeId: 3n,
    name: "fleet-pi-1",
    ipAddresses: ["172.16.60.12"],
    discoveryRanges: ["172.16.60.0-172.16.60.255"],
    version: "v0.5.0",
    commandProtocolUpgradeRequired: true,
    commandProtocolUpgradeRecommended: true,
  }),
  node({
    fleetNodeId: 4n,
    name: "fleet-pi-3",
    ipAddresses: ["172.16.62.39"],
    controlStreamConnected: false,
    lastSeenAt: secondsAgo(8 * 24 * 60 * 60),
    version: "v0.6.2-internal.8",
  }),
  node({
    fleetNodeId: 5n,
    name: "fleet-pi-4",
    enrollmentStatus: FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION,
    pendingEnrollmentId: 11n,
    controlStreamConnected: false,
    lastSeenAt: undefined,
    version: "",
    enrolledBy: "",
  }),
  node({
    fleetNodeId: 6n,
    name: "fleet-pi-5",
    ipAddresses: ["172.16.61.132"],
    discoveryRanges: ["172.16.61.0-172.16.61.255"],
    pairedDeviceCount: 1n,
    version: "v0.6.2-internal.8",
    commandProtocolUpgradeRecommended: true,
    haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE,
    haPeerNodeId: 7n,
    haPeerNodeName: "fleet-pi-6",
    haStandbyReady: true,
  }),
  node({
    fleetNodeId: 7n,
    name: "fleet-pi-6",
    ipAddresses: ["172.16.61.133"],
    discoveryRanges: ["172.16.61.0-172.16.61.255"],
    haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
    haPeerNodeId: 6n,
    haPeerNodeName: "fleet-pi-5",
    haStandbyReady: true,
  }),
];

type NodesFixture = () => FleetNodeSummary[];

const NodesStoryHarness = ({ children, nodes }: { children: ReactNode; nodes: NodesFixture }) => {
  const storyIsReady = useFleetStore(
    (current) =>
      current.auth.username === STORYBOOK_OPERATOR &&
      current.auth.isAuthenticated &&
      NODE_PERMISSIONS.every((permission) => current.auth.permissions.includes(permission)),
  );

  // Auth persists to localStorage, so a reload can render the page on the
  // first pass. A layout effect installs the stub before the page's first fetch.
  useLayoutEffect(() => {
    const previousAuth = useFleetStore.getState().auth;
    const originalListFleetNodes = mutableFleetNodeAdminClient.listFleetNodes;

    mutableFleetNodeAdminClient.listFleetNodes = async () =>
      create(ListFleetNodesResponseSchema, { fleetNodes: nodes() });

    useFleetStore.setState((current) => ({
      auth: {
        ...current.auth,
        isAuthenticated: true,
        permissions: NODE_PERMISSIONS,
        sessionExpiry: new Date(Date.now() + 60 * 60 * 1_000),
        sessionGeneration: current.auth.sessionGeneration + 1,
        username: STORYBOOK_OPERATOR,
      },
    }));

    return () => {
      mutableFleetNodeAdminClient.listFleetNodes = originalListFleetNodes;
      useFleetStore.setState({ auth: previousAuth });
    };
  }, [nodes]);

  return storyIsReady ? <>{children}</> : null;
};

const meta = {
  title: "Proto Fleet/Settings/Nodes",
  component: NodesPage,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story, context) => (
      <NodesStoryHarness nodes={context.parameters.nodes as NodesFixture}>
        <div className="min-h-screen bg-surface-base p-10 phone:p-6">
          <Story />
        </div>
      </NodesStoryHarness>
    ),
  ],
} satisfies Meta<typeof NodesPage>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  parameters: { nodes: allStates },
};

export const Empty: Story = {
  parameters: { nodes: () => [] },
};
