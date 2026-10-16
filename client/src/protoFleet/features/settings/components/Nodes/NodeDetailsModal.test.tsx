import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import NodeDetailsModal from "./NodeDetailsModal";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));
vi.mock("@/shared/utils/utility", async () => {
  const actual = await vi.importActual<typeof import("@/shared/utils/utility")>("@/shared/utils/utility");
  return { ...actual, copyToClipboard: vi.fn() };
});

const mockCopyToClipboard = vi.mocked(copyToClipboard);
const mockPushToast = vi.mocked(pushToast);

const node: FleetNodeItem = {
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
};

const show = (overrides: Partial<FleetNodeItem> = {}) =>
  render(
    <NodeDetailsModal node={{ ...node, ...overrides }} canFindMiners onDismiss={vi.fn()} onFindMiners={vi.fn()} />,
  );

describe("NodeDetailsModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCopyToClipboard.mockResolvedValue(undefined);
  });

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
    expect(screen.getAllByText("—")).toHaveLength(4);
    await waitFor(() => expect(screen.getByText("Never")).toBeVisible());
  });

  it.each([{ controlStreamConnected: false }, { commandProtocolUpgradeRequired: true }])(
    "preserves discovery eligibility for %j",
    (overrides) => {
      show(overrides);
      expect(screen.getByRole("button", { name: "Find miners" })).toBeDisabled();
    },
  );

  it("copies the node id and confirms with a toast", async () => {
    show();
    fireEvent.click(screen.getByText("Technical details"));
    fireEvent.click(screen.getByRole("button", { name: "Copy node id" }));
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({ message: "Node ID copied to clipboard", status: STATUSES.success }),
    );
    expect(mockCopyToClipboard).toHaveBeenCalledWith("7");
  });

  it("reports an error toast when copying the identity fingerprint fails", async () => {
    mockCopyToClipboard.mockRejectedValue(new Error("copy failed"));
    show();
    fireEvent.click(screen.getByText("Technical details"));
    fireEvent.click(screen.getByRole("button", { name: "Copy identity fingerprint" }));
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Failed to copy identity fingerprint",
        status: STATUSES.error,
      }),
    );
    expect(mockCopyToClipboard).toHaveBeenCalledWith(node.identityFingerprint);
  });

  it("marks an empty identity fingerprint as unavailable without a copy button", async () => {
    show({ identityFingerprint: "" });
    fireEvent.click(screen.getByText("Technical details"));
    await waitFor(() => expect(screen.getByText("—")).toBeVisible());
    expect(screen.queryByRole("button", { name: "Copy identity fingerprint" })).not.toBeInTheDocument();
  });
});
