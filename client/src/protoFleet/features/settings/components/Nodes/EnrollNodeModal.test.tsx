import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import EnrollNodeModal from "./EnrollNodeModal";
import {
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
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

const confirmedNode: FleetNodeItem = {
  ...awaitingNode,
  fleetNodeId: "1",
  name: "standalone-node",
  enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
};

const standbyTargetMocks = () => {
  mockListFleetNodes.mockResolvedValue([
    confirmedNode,
    {
      ...awaitingNode,
      fleetNodeId: "2",
      name: "paired-node",
      enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
      haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE,
      haPeerNodeId: "3",
    },
    { ...awaitingNode, fleetNodeId: "4", name: "unconfirmed-node" },
  ]);
};

type GetByRole = ReturnType<typeof render>["getByRole"];

const openStandbyTargets = async (getByRole: GetByRole) => {
  await waitFor(() => expect(getByRole("checkbox", { name: "Enroll as a standby" })).toBeEnabled());
  fireEvent.click(getByRole("checkbox", { name: "Enroll as a standby" }));
  await waitFor(() => expect(getByRole("button", { name: "Standby for" })).toBeEnabled());
  fireEvent.click(getByRole("button", { name: "Standby for" }));
};

const selectStandbyTarget = async (getByRole: GetByRole, name: string) => {
  await openStandbyTargets(getByRole);
  fireEvent.click(await waitFor(() => getByRole("option", { name })));
};

describe("EnrollNodeModal", () => {
  it("only offers confirmed nodes outside an HA pair as standby targets", async () => {
    // Arrange
    standbyTargetMocks();
    const { getByRole, queryByRole } = render(
      <EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    // Act
    await openStandbyTargets(getByRole);

    // Assert
    await waitFor(() => expect(getByRole("option", { name: "standalone-node" })).toBeInTheDocument());
    expect(queryByRole("option", { name: "paired-node" })).not.toBeInTheDocument();
    expect(queryByRole("option", { name: "unconfirmed-node" })).not.toBeInTheDocument();
    expect(getByRole("button", { name: "Continue" })).toBeDisabled();
  });

  it("creates a standby enrollment code for the selected target", async () => {
    // Arrange
    standbyTargetMocks();
    mockCreateEnrollmentCode.mockResolvedValue({ code: "standby-code", pendingEnrollmentId: "12", expiresAt: null });
    const { getByRole, getByText } = render(
      <EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );
    await selectStandbyTarget(getByRole, "standalone-node");

    // Act
    fireEvent.click(getByRole("button", { name: "Continue" }));

    // Assert
    await waitFor(() => expect(mockCreateEnrollmentCode).toHaveBeenCalledWith("1"));
    expect(getByText("standby-code")).toBeInTheDocument();
  });

  describe("when creating the standby enrollment code fails", () => {
    const renderFailedStandbyEnrollment = async () => {
      mockListFleetNodes.mockResolvedValueOnce([confirmedNode]).mockResolvedValue([]);
      mockCreateEnrollmentCode.mockRejectedValue(new Error("Target is no longer eligible"));
      const view = render(<EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);
      await selectStandbyTarget(view.getByRole, "standalone-node");
      return view;
    };

    it("returns to the options step with the error and keeps the checked standby option enabled", async () => {
      // Arrange
      const { getByRole, getByText } = await renderFailedStandbyEnrollment();

      // Act
      fireEvent.click(getByRole("button", { name: "Continue" }));

      // Assert
      await waitFor(() => expect(getByText("Target is no longer eligible")).toBeInTheDocument());
      await waitFor(() => expect(mockListFleetNodes).toHaveBeenCalledTimes(2));
      await waitFor(() => expect(getByText(/No node can take a standby/)).toBeInTheDocument());
      expect(getByRole("checkbox", { name: "Enroll as a standby" })).toBeChecked();
      expect(getByRole("checkbox", { name: "Enroll as a standby" })).toBeEnabled();
      expect(getByRole("button", { name: "Continue" })).toBeDisabled();
    });

    it("lets the user uncheck the standby option to continue as a standalone node", async () => {
      // Arrange
      const { getByRole, getByText } = await renderFailedStandbyEnrollment();
      fireEvent.click(getByRole("button", { name: "Continue" }));
      await waitFor(() => expect(getByText(/No node can take a standby/)).toBeInTheDocument());

      // Act
      fireEvent.click(getByRole("checkbox", { name: "Enroll as a standby" }));

      // Assert
      expect(getByRole("checkbox", { name: "Enroll as a standby" })).not.toBeChecked();
      expect(getByRole("button", { name: "Continue" })).toBeEnabled();
    });
  });

  it("disables the standby option when no node is eligible", async () => {
    // Arrange
    mockListFleetNodes.mockResolvedValue([
      {
        ...awaitingNode,
        fleetNodeId: "2",
        name: "standby-node",
        enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
        haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
        haPeerNodeId: "3",
      },
    ]);

    // Act
    const { getByRole, getByText } = render(
      <EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    // Assert
    await waitFor(() => expect(getByText(/No node can take a standby/)).toBeInTheDocument());
    expect(getByRole("checkbox", { name: "Enroll as a standby" })).toBeDisabled();
    expect(getByRole("button", { name: "Continue" })).toBeEnabled();
  });

  it("retains the standby relationship when resuming confirmation", () => {
    // Arrange
    const resumeNode = {
      ...awaitingNode,
      haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
      haPeerNodeId: "1",
      haPeerNodeName: "active-node",
    };

    // Act
    const { getByText, queryByRole } = render(
      <EnrollNodeModal open resumeNode={resumeNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    // Assert
    expect(getByText("Standby for active-node")).toBeInTheDocument();
    expect(queryByRole("checkbox")).not.toBeInTheDocument();
    expect(mockCreateEnrollmentCode).not.toHaveBeenCalled();
  });

  it("keeps the standalone code visible when an earlier target lookup fails", async () => {
    // Arrange
    let rejectTargets: (error: Error) => void = () => {};
    mockListFleetNodes
      .mockImplementationOnce(
        () =>
          new Promise((_, reject) => {
            rejectTargets = reject;
          }),
      )
      .mockResolvedValue([]);
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

    // Act
    await act(async () => {
      rejectTargets(new Error("Target lookup failed"));
    });

    // Assert
    expect(getByText("pf_code_123")).toBeInTheDocument();
    expect(getByRole("button", { name: "Copy command" })).toBeInTheDocument();
    const instructions = getByRole("link", { name: "Installation instructions" });
    expect(instructions).toHaveAttribute("href", "https://docs.proto.xyz/fleet/fleet-node");
    expect(instructions).toHaveAttribute("target", "_blank");
    expect(instructions).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("advances to the confirm step when a new node registers", async () => {
    // Arrange
    mockListFleetNodes.mockResolvedValue([awaitingNode]);
    mockCreateEnrollmentCode.mockResolvedValue({ code: "pf_code_123", pendingEnrollmentId: "11", expiresAt: null });
    const { getByText } = render(<EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);

    // Act
    fireEvent.click(getByText("Continue"));

    // Assert
    await waitFor(() => {
      expect(getByText("Confirm the node")).toBeInTheDocument();
      expect(getByText("test-node-01")).toBeInTheDocument();
      expect(getByText("abcd1234abcd1234")).toBeInTheDocument();
    });
    expect(mockOnUpdated).toHaveBeenCalled();
  });

  it("ignores awaiting nodes for a different enrollment code", async () => {
    // Arrange
    mockListFleetNodes.mockResolvedValue([{ ...awaitingNode, fleetNodeId: "8", pendingEnrollmentId: "22" }]);
    mockCreateEnrollmentCode.mockResolvedValue({ code: "pf_code_123", pendingEnrollmentId: "11", expiresAt: null });
    const { getByText, queryByText } = render(
      <EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    // Act
    fireEvent.click(getByText("Continue"));

    // Assert
    await waitFor(() => {
      expect(mockListFleetNodes).toHaveBeenCalled();
    });
    expect(queryByText("Confirm the node")).not.toBeInTheDocument();
    expect(getByText("Waiting for the node to register…")).toBeInTheDocument();
  });

  it("opens directly at the confirm step for a resumed node", () => {
    // Act
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    // Assert
    expect(getByText("Confirm the node")).toBeInTheDocument();
    expect(getByText("abcd1234abcd1234")).toBeInTheDocument();
    expect(mockCreateEnrollmentCode).not.toHaveBeenCalled();
  });

  it("shows the one-time api key after confirming", async () => {
    // Arrange
    mockConfirmFleetNode.mockResolvedValue("fleet_test_api_key");
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    // Act
    fireEvent.click(getByText("Confirm node"));

    // Assert
    await waitFor(() => {
      expect(mockConfirmFleetNode).toHaveBeenCalledWith("7", "11");
      expect(getByText("Node confirmed")).toBeInTheDocument();
      expect(getByText("fleet_test_api_key")).toBeInTheDocument();
    });
    expect(getByText(/The service starts after enrollment/)).toBeVisible();
    expect(getByText(/Check that the node is connected in Nodes before adding miners/)).toBeVisible();
  });

  it("notifies and dismisses when Done is clicked after confirming", async () => {
    // Arrange
    mockConfirmFleetNode.mockResolvedValue("fleet_test_api_key");
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );
    fireEvent.click(getByText("Confirm node"));
    await waitFor(() => {
      expect(getByText("fleet_test_api_key")).toBeInTheDocument();
    });
    mockOnUpdated.mockClear();

    // Act
    fireEvent.click(getByText("Done"));

    // Assert
    expect(mockOnUpdated).toHaveBeenCalled();
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("keeps the one-time api key open when Escape is pressed", async () => {
    // Arrange
    mockConfirmFleetNode.mockResolvedValue("fleet_test_api_key");
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );
    fireEvent.click(getByText("Confirm node"));
    await waitFor(() => {
      expect(getByText("fleet_test_api_key")).toBeInTheDocument();
    });

    // Act
    fireEvent.keyDown(document, { key: "Escape" });

    // Assert
    expect(mockOnDismiss).not.toHaveBeenCalled();
    expect(getByText("fleet_test_api_key")).toBeInTheDocument();
  });

  it("revokes the node and dismisses on reject", async () => {
    // Arrange
    mockRevokeFleetNode.mockResolvedValue(undefined);
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );

    // Act
    fireEvent.click(getByText("Reject"));

    // Assert
    await waitFor(() => {
      expect(mockRevokeFleetNode).toHaveBeenCalledWith("7", "11");
      expect(mockOnUpdated).toHaveBeenCalled();
      expect(mockOnDismiss).toHaveBeenCalled();
    });
  });

  it("ignores a stale code response after the modal is dismissed", async () => {
    // Arrange
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

    // Act
    await act(async () => {
      resolveCode?.({ code: "pf_stale_code", pendingEnrollmentId: "11", expiresAt: null });
    });

    // Assert
    expect(queryByText("pf_stale_code")).not.toBeInTheDocument();
    await waitFor(() => {
      expect(getByText("pf_fresh_code")).toBeInTheDocument();
    });
  });
});
