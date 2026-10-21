import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import EnrollNodeModal from "./EnrollNodeModal";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";

vi.mock("@/protoFleet/api/useFleetNodes");
vi.mock("@/shared/features/toaster");

const mockListFleetNodes = vi.fn();
const mockCreateEnrollmentCode = vi.fn();
const mockConfirmFleetNode = vi.fn();
const mockRevokeFleetNode = vi.fn();
const mockOnDismiss = vi.fn();
const mockOnUpdated = vi.fn();

const awaitingNode: FleetNodeItem = {
  fleetNodeId: "7",
  pendingEnrollmentId: "11",
  name: "test-node-01",
  enrollmentStatus: FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION,
  identityFingerprint: "abcd1234abcd1234",
  commandProtocolUpgradeRequired: false,
  commandProtocolUpgradeRecommended: false,
  controlStreamConnected: false,
  pairedDeviceCount: 0,
  standbyEnrollmentEligible: false,
  standbyEnrollmentUnavailableReason: "",
  standbyForNodeId: null,
  standbyForNodeName: "",
  haRole: "",
  haStatusReason: "",
  haOwnerNodeId: null,
  ipAddresses: [],
  version: "",
  enrolledBy: "",
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: null,
};

afterEach(() => vi.restoreAllMocks());

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    x: 100,
    y: 100,
    top: 100,
    left: 100,
    right: 400,
    bottom: 148,
    width: 300,
    height: 48,
    toJSON: () => ({}),
  });
  vi.mocked(useFleetNodes).mockReturnValue({
    listFleetNodes: mockListFleetNodes,
    createEnrollmentCode: mockCreateEnrollmentCode,
    confirmFleetNode: mockConfirmFleetNode,
    revokeFleetNode: mockRevokeFleetNode,
    listFleetNodeDevices: vi.fn(),
    listFleetNodeDiscoveredDevices: vi.fn(),
    discoverOnFleetNode: vi.fn(),
    pairDiscoveredDevicesOnFleetNode: vi.fn(),
  });

  vi.clearAllMocks();
});

describe("EnrollNodeModal", () => {
  it("requires a server-eligible target before minting a standby code", async () => {
    mockListFleetNodes.mockResolvedValue([
      {
        ...awaitingNode,
        fleetNodeId: "1",
        name: "active-node",
        enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
        standbyEnrollmentEligible: true,
        haRole: "active",
      },
      {
        ...awaitingNode,
        fleetNodeId: "2",
        name: "unavailable-node",
        enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
        standbyEnrollmentEligible: false,
      },
    ]);
    mockCreateEnrollmentCode.mockResolvedValue({ code: "standby-code", pendingEnrollmentId: "12", expiresAt: null });
    const { getByRole, getByText, queryByRole } = render(
      <EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );
    fireEvent.click(getByRole("checkbox", { name: "Enroll as a standby" }));
    expect(getByRole("button", { name: "Continue" })).toBeDisabled();
    await waitFor(() => expect(getByRole("button", { name: "Standby for" })).toBeEnabled());
    fireEvent.click(getByRole("button", { name: "Standby for" }));
    await waitFor(() => expect(getByRole("option", { name: "active-node" })).toBeInTheDocument());
    expect(queryByRole("option", { name: "unavailable-node" })).not.toBeInTheDocument();
    fireEvent.click(getByRole("option", { name: "active-node" }));
    fireEvent.click(getByRole("button", { name: "Continue" }));
    await waitFor(() => expect(mockCreateEnrollmentCode).toHaveBeenCalledWith("1"));
    expect(getByText("standby-code")).toBeInTheDocument();
  });

  it("retains the standby relationship when resuming confirmation", () => {
    const { getByText, queryByRole } = render(
      <EnrollNodeModal
        open
        resumeNode={{ ...awaitingNode, standbyForNodeId: "1", standbyForNodeName: "active-node" }}
        onDismiss={mockOnDismiss}
        onUpdated={mockOnUpdated}
      />,
    );
    expect(getByText("Standby for active-node")).toBeInTheDocument();
    expect(queryByRole("checkbox")).not.toBeInTheDocument();
    expect(mockCreateEnrollmentCode).not.toHaveBeenCalled();
  });

  it("waits for Continue before minting a standalone code and showing the enroll command", async () => {
    mockListFleetNodes.mockResolvedValue([]);
    mockCreateEnrollmentCode.mockResolvedValue({ code: "pf_code_123", pendingEnrollmentId: "11", expiresAt: null });

    const { getByRole, getByText } = render(
      <EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    expect(mockCreateEnrollmentCode).not.toHaveBeenCalled();
    expect(getByRole("checkbox", { name: "Enroll as a standby" })).not.toBeChecked();
    fireEvent.click(getByRole("button", { name: "Continue" }));

    await waitFor(() => {
      const command = getByRole("button", { name: "Copy command" }).parentElement;
      expect(command).toHaveTextContent("sudo fleetnode-enroll");
      expect(command).toHaveTextContent("--server-url=http://localhost:4000");
      expect(getByText("pf_code_123")).toBeInTheDocument();
      expect(getByText("Waiting for the node to register…")).toBeInTheDocument();
    });
    const instructions = getByRole("link", { name: "Installation instructions" });
    expect(instructions).toHaveAttribute("href", "https://docs.proto.xyz/fleet/fleet-node");
    expect(instructions).toHaveAttribute("target", "_blank");
    expect(instructions).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("advances to the confirm step when a new node registers", async () => {
    mockListFleetNodes.mockResolvedValue([awaitingNode]);
    mockCreateEnrollmentCode.mockResolvedValue({ code: "pf_code_123", pendingEnrollmentId: "11", expiresAt: null });

    const { getByText } = render(<EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);
    fireEvent.click(getByText("Continue"));

    await waitFor(() => {
      expect(getByText("Confirm the node")).toBeInTheDocument();
      expect(getByText("test-node-01")).toBeInTheDocument();
      expect(getByText("abcd1234abcd1234")).toBeInTheDocument();
    });
    expect(mockOnUpdated).toHaveBeenCalled();
  });

  it("ignores awaiting nodes for a different enrollment code", async () => {
    mockListFleetNodes.mockResolvedValue([{ ...awaitingNode, fleetNodeId: "8", pendingEnrollmentId: "22" }]);
    mockCreateEnrollmentCode.mockResolvedValue({ code: "pf_code_123", pendingEnrollmentId: "11", expiresAt: null });

    const { getByText, queryByText } = render(
      <EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    fireEvent.click(getByText("Continue"));
    await waitFor(() => {
      expect(mockListFleetNodes).toHaveBeenCalled();
    });
    expect(queryByText("Confirm the node")).not.toBeInTheDocument();
    expect(getByText("Waiting for the node to register…")).toBeInTheDocument();
  });

  it("opens directly at the confirm step for a resumed node", () => {
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    expect(getByText("Confirm the node")).toBeInTheDocument();
    expect(getByText("abcd1234abcd1234")).toBeInTheDocument();
    expect(mockCreateEnrollmentCode).not.toHaveBeenCalled();
  });

  it("shows the one-time api key after confirming", async () => {
    mockConfirmFleetNode.mockResolvedValue("fleet_test_api_key");
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    fireEvent.click(getByText("Confirm node"));

    await waitFor(() => {
      expect(mockConfirmFleetNode).toHaveBeenCalledWith("7", "11");
      expect(getByText("Node confirmed")).toBeInTheDocument();
      expect(getByText("fleet_test_api_key")).toBeInTheDocument();
    });

    expect(getByText(/The service starts after enrollment/)).toBeVisible();
    expect(getByText(/Check that the node is connected in Nodes before adding miners/)).toBeVisible();

    fireEvent.click(getByText("Done"));

    expect(mockOnUpdated).toHaveBeenCalled();
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("keeps the one-time api key open when Escape is pressed", async () => {
    mockConfirmFleetNode.mockResolvedValue("fleet_test_api_key");
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );
    fireEvent.click(getByText("Confirm node"));
    await waitFor(() => {
      expect(getByText("fleet_test_api_key")).toBeInTheDocument();
    });

    fireEvent.keyDown(document, { key: "Escape" });

    expect(mockOnDismiss).not.toHaveBeenCalled();
    expect(getByText("fleet_test_api_key")).toBeInTheDocument();
  });

  it("revokes the node and dismisses on reject", async () => {
    mockRevokeFleetNode.mockResolvedValue(undefined);
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    fireEvent.click(getByText("Reject"));

    await waitFor(() => {
      expect(mockRevokeFleetNode).toHaveBeenCalledWith("7", "11");
      expect(mockOnUpdated).toHaveBeenCalled();
      expect(mockOnDismiss).toHaveBeenCalled();
    });
  });

  it("ignores a stale code response after the modal is dismissed", async () => {
    let resolveCode:
      ((value: { code: string; pendingEnrollmentId: string; expiresAt: Date | null }) => void) | undefined;
    mockListFleetNodes.mockResolvedValue([]);
    mockCreateEnrollmentCode
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveCode = resolve;
          }),
      )
      .mockResolvedValue({ code: "pf_fresh_code", pendingEnrollmentId: "12", expiresAt: null });

    const { getByText, queryByText, rerender } = render(
      <EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );
    fireEvent.click(getByText("Continue"));
    await waitFor(() => {
      expect(mockCreateEnrollmentCode).toHaveBeenCalled();
    });

    rerender(<EnrollNodeModal open={false} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);
    rerender(<EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);
    fireEvent.click(getByText("Continue"));
    await act(async () => {
      resolveCode?.({ code: "pf_stale_code", pendingEnrollmentId: "11", expiresAt: null });
    });

    expect(queryByText("pf_stale_code")).not.toBeInTheDocument();
    await waitFor(() => {
      expect(getByText("pf_fresh_code")).toBeInTheDocument();
    });
  });
});
