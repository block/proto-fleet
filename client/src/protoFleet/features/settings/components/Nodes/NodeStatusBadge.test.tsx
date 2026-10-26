import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import NodeStatusBadge from "./NodeStatusBadge";
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
  controlStreamConnected: false,
  pairedDeviceCount: 0,
  haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
  haPeerNodeId: null,
  haPeerNodeName: "",
  haStandbyReady: false,
  haStatusReason: "",
  ipAddresses: [],
  version: "",
  enrolledBy: "",
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: null,
};

afterEach(() => {
  vi.useRealTimers();
});

describe("NodeStatusBadge", () => {
  it.each([
    ["Connected", 30_000],
    ["Connected", 120_000],
    ["Stale", 120_001],
  ])("shows %s for a confirmed node last seen %dms ago", (label, ageMs) => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-09T12:10:00Z"));
    const node = {
      ...baseNode,
      controlStreamConnected: true,
      commandProtocolUpgradeRecommended: label === "Stale",
      lastSeenAt: new Date(Date.now() - ageMs),
    };

    const { getByText } = render(<NodeStatusBadge node={node} />);

    expect(getByText(label)).toBeInTheDocument();
  });

  it("shows Heartbeat only when a fresh heartbeat has no control stream", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-09T12:10:00Z"));
    const node = { ...baseNode, lastSeenAt: new Date(Date.now() - 30_000) };

    const { getByText } = render(<NodeStatusBadge node={node} />);

    expect(getByText("Heartbeat only")).toBeInTheDocument();
  });

  it("exposes the Stale description as screen-reader text", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-09T12:10:00Z"));
    const node = { ...baseNode, controlStreamConnected: true, lastSeenAt: new Date(Date.now() - 120_001) };

    const { getByText } = render(<NodeStatusBadge node={node} />);

    expect(getByText("Stale")).toBeInTheDocument();
    expect(getByText("No heartbeat received in the last 2 minutes.")).toHaveClass("sr-only");
  });

  it("adds no screen-reader description to a Connected badge", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-09T12:10:00Z"));
    const node = { ...baseNode, controlStreamConnected: true, lastSeenAt: new Date(Date.now() - 30_000) };

    const { container, getByText } = render(<NodeStatusBadge node={node} />);

    expect(getByText("Connected")).toBeInTheDocument();
    expect(container.querySelector(".sr-only")).toBeNull();
  });

  it("ignores upgrade recommendations for a node without a control stream or heartbeat", () => {
    const { getByText } = render(<NodeStatusBadge node={{ ...baseNode, commandProtocolUpgradeRecommended: true }} />);

    expect(getByText("Never connected")).toBeInTheDocument();
  });

  it.each([
    ["Upgrade required", true, false],
    ["Upgrade recommended", false, true],
    ["Upgrade required", true, true],
  ])("shows %s before the first heartbeat (required=%s, recommended=%s)", (label, required, recommended) => {
    const node = {
      ...baseNode,
      commandProtocolUpgradeRequired: required,
      commandProtocolUpgradeRecommended: recommended,
      controlStreamConnected: true,
    };

    const { getByText } = render(<NodeStatusBadge node={node} />);

    expect(getByText(label)).toBeInTheDocument();
  });

  it("shows Awaiting confirmation for a registered node", () => {
    const { getByText } = render(
      <NodeStatusBadge node={{ ...baseNode, enrollmentStatus: FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION }} />,
    );

    expect(getByText("Awaiting confirmation")).toBeInTheDocument();
  });
});
