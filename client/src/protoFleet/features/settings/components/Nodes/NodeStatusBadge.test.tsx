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
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: null,
};

afterEach(() => {
  vi.useRealTimers();
});

describe("NodeStatusBadge", () => {
  it.each([
    ["Online", 30_000],
    ["Stale", 120_000],
  ])("shows %s for a confirmed node last seen %dms ago", (label, ageMs) => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-09T12:10:00Z"));
    const node = { ...baseNode, lastSeenAt: new Date(Date.now() - ageMs) };

    const { getByText } = render(<NodeStatusBadge node={node} />);

    expect(getByText(label)).toBeInTheDocument();
  });

  it("shows Never connected for a confirmed node without a heartbeat", () => {
    const { getByText } = render(<NodeStatusBadge node={baseNode} />);

    expect(getByText("Never connected")).toBeInTheDocument();
  });

  it("shows Awaiting confirmation for a registered node", () => {
    const { getByText } = render(
      <NodeStatusBadge node={{ ...baseNode, enrollmentStatus: FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION }} />,
    );

    expect(getByText("Awaiting confirmation")).toBeInTheDocument();
  });
});
