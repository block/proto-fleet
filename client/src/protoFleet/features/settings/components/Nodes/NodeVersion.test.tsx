import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import NodeVersion from "./NodeVersion";
import {
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

const baseNode: FleetNodeItem = {
  fleetNodeId: "1",
  pendingEnrollmentId: null,
  name: "test-node",
  enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
  identityFingerprint: "abcd1234abcd1234",
  commandProtocolUpgradeRequired: false,
  commandProtocolUpgradeRecommended: false,
  controlStreamConnected: true,
  discoveryCoverageDegraded: false,
  pairedDeviceCount: 0,
  haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
  haPeerNodeId: null,
  haPeerNodeName: "",
  haStandbyReady: false,
  haStatusReason: "",
  ipAddresses: [],
  version: "v0.6.2",
  enrolledBy: "",
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: new Date("2026-07-09T12:01:00Z"),
};

describe("NodeVersion", () => {
  it("shows an up-to-date version without an upgrade indicator", () => {
    render(<NodeVersion node={baseNode} />);

    expect(screen.getByText("v0.6.2")).toBeInTheDocument();
    expect(screen.queryByTestId("alert-icon")).not.toBeInTheDocument();
  });

  it("marks a missing version", () => {
    render(<NodeVersion node={{ ...baseNode, version: "" }} />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it.each([
    ["Upgrade recommended", "text-intent-warning-fill", { commandProtocolUpgradeRecommended: true }],
    [
      "Upgrade required",
      "text-intent-critical-fill",
      { commandProtocolUpgradeRequired: true, commandProtocolUpgradeRecommended: true },
    ],
  ])("flags %s beside the version, named for hover and screen readers", (label, iconClass, overrides) => {
    render(<NodeVersion node={{ ...baseNode, ...overrides }} />);

    expect(screen.getByTestId("alert-icon")).toHaveClass(iconClass);
    expect(screen.getByText(label)).toHaveClass("sr-only");
    expect(screen.getByTitle(label)).toHaveTextContent("v0.6.2");
  });

  it("spells the upgrade state out when asked to", () => {
    render(<NodeVersion node={{ ...baseNode, commandProtocolUpgradeRecommended: true }} showUpgradeLabel />);

    expect(screen.getByText("Upgrade recommended")).not.toHaveClass("sr-only");
    expect(screen.queryByTitle("Upgrade recommended")).not.toBeInTheDocument();
  });

  it.each([
    ["has no control stream", { controlStreamConnected: false }],
    ["is not confirmed", { enrollmentStatus: FleetNodeEnrollmentStatus.REVOKED }],
  ])("ignores upgrade flags for a node that %s", (_state, overrides) => {
    render(
      <NodeVersion
        node={{
          ...baseNode,
          commandProtocolUpgradeRequired: true,
          commandProtocolUpgradeRecommended: true,
          ...overrides,
        }}
      />,
    );

    expect(screen.queryByTestId("alert-icon")).not.toBeInTheDocument();
  });
});
