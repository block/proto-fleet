import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import NodeDetailsModal from "./NodeDetailsModal";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

const node = {
  fleetNodeId: "7",
  pendingEnrollmentId: null,
  name: "node-01",
  enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
  identityFingerprint: "abcd1234abcd1234",
  commandProtocolUpgradeRequired: false,
  commandProtocolUpgradeRecommended: false,
  controlStreamConnected: true,
  pairedDeviceCount: 12,
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: new Date(),
  ipAddresses: ["192.168.1.7", "fd00::7"],
  version: "v0.5.1-internal.3",
  enrolledBy: "operator@example.com",
} as FleetNodeItem;

const show = (overrides: Partial<FleetNodeItem> = {}) =>
  render(
    <NodeDetailsModal node={{ ...node, ...overrides }} canFindMiners onDismiss={vi.fn()} onFindMiners={vi.fn()} />,
  );

describe("NodeDetailsModal", () => {
  it("shows host metadata and enrollment attribution, with technical identity collapsed", async () => {
    show();
    await waitFor(() => expect(screen.getByText("192.168.1.7")).toBeVisible());
    expect(screen.getByText("fd00::7")).toBeVisible();
    expect(screen.getByText("v0.5.1-internal.3")).toBeVisible();
    expect(screen.getByText("operator@example.com")).toBeVisible();
    expect(screen.getByText("12")).toBeVisible();
    expect(screen.getByText("Registered")).toBeVisible();
    expect(screen.getByText(node.createdAt!.toLocaleString())).toBeVisible();
    expect(screen.getByText("Technical details").closest("details")).not.toHaveAttribute("open");
    fireEvent.click(screen.getByText("Technical details"));
    expect(screen.getByText(node.identityFingerprint)).toBeVisible();
  });

  it("explicitly marks missing metadata and registration time", async () => {
    show({ ipAddresses: [], version: "", enrolledBy: "", createdAt: null, lastSeenAt: null });
    expect(screen.getAllByText("Unavailable")).toHaveLength(4);
    await waitFor(() => expect(screen.getByText("Never")).toBeVisible());
  });

  it.each([{ controlStreamConnected: false }, { commandProtocolUpgradeRequired: true }])(
    "preserves discovery eligibility for %j",
    (overrides) => {
      show(overrides);
      expect(screen.getByRole("button", { name: "Find miners" })).toBeDisabled();
    },
  );
});
