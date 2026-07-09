import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { buildFleetNodeEnrollCommand } from "./enrollNodeCommand";
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
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: null,
};

beforeEach(() => {
  vi.mocked(useFleetNodes).mockReturnValue({
    listFleetNodes: mockListFleetNodes,
    createEnrollmentCode: mockCreateEnrollmentCode,
    confirmFleetNode: mockConfirmFleetNode,
    revokeFleetNode: mockRevokeFleetNode,
  });

  vi.clearAllMocks();
});

describe("buildFleetNodeEnrollCommand", () => {
  it.each([
    {
      label: "LAN HTTP",
      location: {
        origin: "http://192.168.1.20:8080",
        protocol: "http:",
        hostname: "192.168.1.20",
      },
      expected: "fleetnode enroll --server-url=http://192.168.1.20:4000 --allow-insecure-transport",
    },
    {
      label: "HTTPS",
      location: {
        origin: "https://fleet.example.com",
        protocol: "https:",
        hostname: "fleet.example.com",
      },
      expected: "fleetnode enroll --server-url=https://fleet.example.com/api-proxy",
    },
    {
      label: "localhost HTTP",
      location: {
        origin: "http://localhost:8080",
        protocol: "http:",
        hostname: "localhost",
      },
      expected: "fleetnode enroll --server-url=http://localhost:4000",
    },
    {
      label: "127.0.0.1 HTTP",
      location: {
        origin: "http://127.0.0.1:8080",
        protocol: "http:",
        hostname: "127.0.0.1",
      },
      expected: "fleetnode enroll --server-url=http://127.0.0.1:4000",
    },
    {
      label: "::1 HTTP",
      location: {
        origin: "http://[::1]:8080",
        protocol: "http:",
        hostname: "[::1]",
      },
      expected: "fleetnode enroll --server-url=http://[::1]:4000",
    },
  ])("builds the enroll command for $label", ({ location, expected }) => {
    expect(buildFleetNodeEnrollCommand(location)).toBe(expected);
  });
});

describe("EnrollNodeModal", () => {
  it("mints a code on open and shows the enroll command", async () => {
    mockListFleetNodes.mockResolvedValue([]);
    mockCreateEnrollmentCode.mockResolvedValue({ code: "pf_code_123", pendingEnrollmentId: "11", expiresAt: null });

    const { getByText } = render(<EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);

    await waitFor(() => {
      expect(getByText(/fleetnode enroll --server-url=/)).toBeInTheDocument();
      expect(getByText("pf_code_123")).toBeInTheDocument();
      expect(getByText("Waiting for the node to register…")).toBeInTheDocument();
    });
  });

  it("advances to the confirm step when a new node registers", async () => {
    mockListFleetNodes.mockResolvedValue([awaitingNode]);
    mockCreateEnrollmentCode.mockResolvedValue({ code: "pf_code_123", pendingEnrollmentId: "11", expiresAt: null });

    const { getByText } = render(<EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);

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

    fireEvent.click(getByText("Fingerprint matches — confirm"));

    await waitFor(() => {
      expect(mockConfirmFleetNode).toHaveBeenCalledWith("7", "11");
      expect(getByText("Node confirmed")).toBeInTheDocument();
      expect(getByText("fleet_test_api_key")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Done"));

    expect(mockOnUpdated).toHaveBeenCalled();
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("keeps the one-time api key open when Escape is pressed", async () => {
    mockConfirmFleetNode.mockResolvedValue("fleet_test_api_key");
    const { getByText } = render(
      <EnrollNodeModal open resumeNode={awaitingNode} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />,
    );
    fireEvent.click(getByText("Fingerprint matches — confirm"));
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
    await waitFor(() => {
      expect(mockCreateEnrollmentCode).toHaveBeenCalled();
    });

    rerender(<EnrollNodeModal open={false} onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);
    rerender(<EnrollNodeModal open onDismiss={mockOnDismiss} onUpdated={mockOnUpdated} />);
    await act(async () => {
      resolveCode?.({ code: "pf_stale_code", pendingEnrollmentId: "11", expiresAt: null });
    });

    expect(queryByText("pf_stale_code")).not.toBeInTheDocument();
    await waitFor(() => {
      expect(getByText("pf_fresh_code")).toBeInTheDocument();
    });
  });
});
