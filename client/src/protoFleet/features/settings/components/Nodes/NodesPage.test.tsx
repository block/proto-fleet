import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import NodesPage from "./NodesPage";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import type { ListAction, ListActionValue } from "@/shared/components/List/types";

const permissionsMock = vi.hoisted(() => ({ current: [] as string[] }));
const listFleetNodesMock = vi.hoisted(() => vi.fn());
const revokeFleetNodeMock = vi.hoisted(() => vi.fn());
const listFleetNodeDevicesMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (permission: string) => permissionsMock.current.includes(permission),
}));

vi.mock("@/protoFleet/api/useFleetNodes", () => ({
  useFleetNodes: () => ({
    listFleetNodes: listFleetNodesMock,
    createEnrollmentCode: vi.fn(),
    confirmFleetNode: vi.fn(),
    revokeFleetNode: revokeFleetNodeMock,
    listFleetNodeDevices: listFleetNodeDevicesMock,
  }),
}));

vi.mock("@/shared/hooks/usePoll", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  return {
    usePoll: ({ enabled, fetchData }: { enabled?: boolean; fetchData: () => Promise<void> | void }) => {
      React.useEffect(() => {
        if (enabled !== false) {
          void fetchData();
        }
      }, [enabled, fetchData]);
    },
  };
});

vi.mock("@/protoFleet/features/settings/components/Nodes/EnrollNodeModal", () => ({
  default: ({ open, resumeNode }: { open: boolean; resumeNode?: FleetNodeItem | null }) =>
    open ? (
      <div data-testid="enroll-node-modal">{resumeNode ? `Resume ${resumeNode.name}` : "New enrollment"}</div>
    ) : null,
}));

vi.mock("@/protoFleet/features/settings/components/Nodes/NodeDetailsModal", () => ({
  default: ({
    node,
    blockedPairingIdentifiers,
    onPairingStarted,
    onPairingCompleted,
    onDismiss,
  }: {
    node: FleetNodeItem;
    blockedPairingIdentifiers: string[];
    onPairingStarted: (identifiers: string[]) => void;
    onPairingCompleted: (identifiers: string[]) => void;
    onDismiss: () => void;
  }) => (
    <div role="dialog" aria-label="Node details">
      {node.name}
      <span>{blockedPairingIdentifiers.length} blocked miners</span>
      <button type="button" onClick={() => onPairingStarted(["miner-2"])}>
        Start pairing
      </button>
      <button type="button" onClick={() => onPairingCompleted(["miner-2"])}>
        Complete pairing
      </button>
      <button type="button" onClick={onDismiss}>
        Close details
      </button>
    </div>
  ),
}));

vi.mock("@/protoFleet/features/settings/components/Nodes/RevokeNodeDialog", () => ({
  default: ({
    open,
    nodeName,
    affectedMiners,
    isLoadingImpact,
    impactError,
    onConfirm,
  }: {
    open: boolean;
    nodeName: string;
    affectedMiners: string[];
    isLoadingImpact: boolean;
    impactError: string;
    onConfirm: () => void;
    onDismiss: () => void;
    isSubmitting: boolean;
  }) =>
    open ? (
      <div role="dialog" aria-label="Revoke node">
        <div>{nodeName}</div>
        <div>{affectedMiners.length} affected miners</div>
        <button type="button" onClick={onConfirm} disabled={isLoadingImpact || !!impactError}>
          Confirm revoke
        </button>
      </div>
    ) : null,
}));

vi.mock("@/shared/components/List", () => {
  const resolveActionValue = <Value,>(
    value: ListActionValue<FleetNodeItem, Value> | undefined,
    item: FleetNodeItem,
  ): Value | undefined => (typeof value === "function" ? (value as (item: FleetNodeItem) => Value)(item) : value);

  return {
    default: ({
      items,
      noDataElement,
      actions,
      onRowClick,
    }: {
      items: FleetNodeItem[];
      noDataElement?: ReactNode;
      actions?: ListAction<FleetNodeItem>[];
      onRowClick?: (node: FleetNodeItem) => void;
    }) => (
      <div data-testid="nodes-list">
        {items.length === 0
          ? noDataElement
          : items.map((node) => (
              <div key={node.fleetNodeId}>
                <span>{node.name}</span>
                {onRowClick ? (
                  <button type="button" onClick={() => onRowClick(node)}>
                    View {node.name}
                  </button>
                ) : null}
                {actions?.map((action, index) => {
                  if (resolveActionValue(action.hidden, node)) {
                    return null;
                  }

                  const title = resolveActionValue(action.title, node);
                  return (
                    <button key={`${title}-${index}`} type="button" onClick={() => action.actionHandler(node)}>
                      {title}
                    </button>
                  );
                })}
              </div>
            ))}
      </div>
    ),
  };
});

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { error: "error", success: "success" },
}));

const confirmedNode: FleetNodeItem = {
  fleetNodeId: "7",
  pendingEnrollmentId: null,
  name: "node-01",
  enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
  identityFingerprint: "abcd1234abcd1234",
  commandProtocolUpgradeRequired: false,
  controlStreamConnected: true,
  pairedDeviceCount: 0,
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: new Date("2026-07-09T12:01:00Z"),
};

const awaitingNode: FleetNodeItem = {
  ...confirmedNode,
  pendingEnrollmentId: "11",
  name: "node-pending",
  enrollmentStatus: FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION,
};

const renderNodesPage = () =>
  render(
    <MemoryRouter>
      <NodesPage />
    </MemoryRouter>,
  );

describe("NodesPage", () => {
  beforeEach(() => {
    permissionsMock.current = ["fleetnode:read", "fleetnode:manage"];
    listFleetNodesMock.mockReset();
    listFleetNodesMock.mockResolvedValue([confirmedNode]);
    revokeFleetNodeMock.mockReset();
    revokeFleetNodeMock.mockResolvedValue(undefined);
    listFleetNodeDevicesMock.mockReset();
    listFleetNodeDevicesMock.mockResolvedValue([]);
  });

  it("renders nodes for read-only users without management actions", async () => {
    permissionsMock.current = ["fleetnode:read"];

    renderNodesPage();

    expect(await screen.findByText("node-01")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Enroll node" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Confirm enrollment" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Revoke" })).not.toBeInTheDocument();
  });

  it("shows management actions for users with fleetnode manage permission", async () => {
    listFleetNodesMock.mockResolvedValue([awaitingNode]);

    renderNodesPage();

    expect(await screen.findByText("node-pending")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Enroll node" }));
    expect(screen.getByTestId("enroll-node-modal")).toHaveTextContent("New enrollment");

    fireEvent.click(screen.getByRole("button", { name: "Confirm enrollment" }));
    expect(screen.getByTestId("enroll-node-modal")).toHaveTextContent("Resume node-pending");
  });

  it("passes the pending enrollment id when revoking an awaiting node", async () => {
    listFleetNodesMock.mockResolvedValue([awaitingNode]);

    renderNodesPage();

    expect(await screen.findByText("node-pending")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    expect(screen.getByRole("dialog", { name: "Revoke node" })).toBeInTheDocument();

    await waitFor(() => expect(screen.getByRole("button", { name: "Confirm revoke" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));

    await waitFor(() => expect(revokeFleetNodeMock).toHaveBeenCalledWith("7", "11"));
  });

  it("shows status totals and opens Node details", async () => {
    listFleetNodesMock.mockResolvedValue([{ ...confirmedNode, lastSeenAt: new Date() }]);
    renderNodesPage();

    expect(await screen.findByText("node-01")).toBeInTheDocument();
    expect(screen.getByLabelText("Node status totals")).toHaveTextContent("Connected1");
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    expect(screen.getByRole("dialog", { name: "Node details" })).toHaveTextContent("node-01");
  });

  it("keeps uncertain pairing identifiers blocked across detail dismissal", async () => {
    renderNodesPage();

    await screen.findByText("node-01");
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Start pairing" }));
    expect(screen.getByRole("dialog", { name: "Node details" })).toHaveTextContent("1 blocked miners");

    fireEvent.click(screen.getByRole("button", { name: "Close details" }));
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    expect(screen.getByRole("dialog", { name: "Node details" })).toHaveTextContent("1 blocked miners");

    fireEvent.click(screen.getByRole("button", { name: "Complete pairing" }));
    expect(screen.getByRole("dialog", { name: "Node details" })).toHaveTextContent("0 blocked miners");
  });

  it("loads the affected miners before allowing revocation", async () => {
    listFleetNodeDevicesMock.mockResolvedValue([{ deviceIdentifier: "miner-1" }, { deviceIdentifier: "miner-2" }]);
    renderNodesPage();

    expect(await screen.findByText("node-01")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: "Revoke node" })).toHaveTextContent("2 affected miners"),
    );
    expect(listFleetNodeDevicesMock).toHaveBeenCalledWith("7");
  });
});
