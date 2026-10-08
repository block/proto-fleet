import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import NodeStatusBadge from "./NodeStatusBadge";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
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
    const node = { ...baseNode, controlStreamConnected: true, lastSeenAt: new Date(Date.now() - ageMs) };

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

  it("keeps stale heartbeat status ahead of an upgrade recommendation", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-09T12:10:00Z"));
    const { getByText } = render(
      <NodeStatusBadge
        node={{
          ...baseNode,
          controlStreamConnected: true,
          commandProtocolUpgradeRecommended: true,
          lastSeenAt: baseNode.createdAt,
        }}
      />,
    );
    expect(getByText("Stale")).toBeInTheDocument();
  });

  it("shows Never connected for a confirmed node without a heartbeat", () => {
    const { getByText } = render(<NodeStatusBadge node={baseNode} />);

    expect(getByText("Never connected")).toBeInTheDocument();
  });

  it("shows an upgrade warning for an active node even before its first heartbeat", () => {
    const node = {
      ...baseNode,
      commandProtocolUpgradeRequired: true,
      controlStreamConnected: true,
    };

    const { getByText } = render(<NodeStatusBadge node={node} />);

    expect(getByText("Upgrade required")).toBeInTheDocument();
  });

  it("shows Awaiting confirmation for a registered node", () => {
    const { getByText } = render(
      <NodeStatusBadge node={{ ...baseNode, enrollmentStatus: FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION }} />,
    );

    expect(getByText("Awaiting confirmation")).toBeInTheDocument();
  });

  it.each([
    [true, false, "Upgrade recommended"],
    [true, true, "Upgrade required"],
    [false, false, "Never connected"],
  ])("separates recommendations from required upgrades and disconnection (%s, %s)", (connected, required, label) => {
    const { getByText } = render(
      <NodeStatusBadge
        node={{
          ...baseNode,
          controlStreamConnected: connected,
          commandProtocolUpgradeRequired: required,
          commandProtocolUpgradeRecommended: true,
        }}
      />,
    );
    expect(getByText(label)).toBeInTheDocument();
  });
});
