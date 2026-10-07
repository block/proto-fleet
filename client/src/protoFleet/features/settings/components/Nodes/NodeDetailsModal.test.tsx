import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import NodeDetailsModal from "./NodeDetailsModal";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

const node: FleetNodeItem = {
  fleetNodeId: "7",
  pendingEnrollmentId: null,
  name: "node-01",
  enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
  identityFingerprint: "abcd1234abcd1234",
  commandProtocolUpgradeRequired: false,
  controlStreamConnected: true,
  pairedDeviceCount: 1,
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: new Date(),
};

describe("NodeDetailsModal", () => {
  it("shows Node details and opens the finder without miner management sections", () => {
    const onFindMiners = vi.fn();
    render(<NodeDetailsModal node={node} canFindMiners onDismiss={vi.fn()} onFindMiners={onFindMiners} />);

    expect(screen.getByText("Identity fingerprint")).toBeInTheDocument();
    expect(screen.getByText("abcd1234abcd1234")).toBeInTheDocument();
    expect(screen.queryByText(/Paired miners/)).not.toBeInTheDocument();
    expect(screen.queryByText("Discovered miners")).not.toBeInTheDocument();
    expect(screen.queryByText("Pair selected miners")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Scan target")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    expect(onFindMiners).toHaveBeenCalledTimes(1);
  });

  it("disables finding when the Node command connection is unavailable", () => {
    render(
      <NodeDetailsModal
        node={{ ...node, controlStreamConnected: false }}
        canFindMiners
        onDismiss={vi.fn()}
        onFindMiners={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Find miners" })).toBeDisabled();
  });
});
