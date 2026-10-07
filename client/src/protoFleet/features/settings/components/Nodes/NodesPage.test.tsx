import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import NodesPage from "./NodesPage";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DevicePairingResultSchema,
  FleetNodeDiscoveredDeviceSchema,
  FleetNodeEnrollmentStatus,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import type { ListAction, ListActionValue } from "@/shared/components/List/types";

const permissionsMock = vi.hoisted(() => ({ current: [] as string[] }));
const listFleetNodesMock = vi.hoisted(() => vi.fn());
const revokeFleetNodeMock = vi.hoisted(() => vi.fn());
const listFleetNodeDevicesMock = vi.hoisted(() => vi.fn());
const listFleetNodeDiscoveredDevicesMock = vi.hoisted(() => vi.fn());
const discoverOnFleetNodeMock = vi.hoisted(() => vi.fn());
const pairDiscoveredDevicesOnFleetNodeMock = vi.hoisted(() => vi.fn());

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
    listFleetNodeDiscoveredDevices: listFleetNodeDiscoveredDevicesMock,
    discoverOnFleetNode: discoverOnFleetNodeMock,
    pairDiscoveredDevicesOnFleetNode: pairDiscoveredDevicesOnFleetNodeMock,
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

vi.mock("@/protoFleet/features/settings/components/Nodes/NodeDetailsModal", () => {
  return {
    default: function MockNodeDetailsModal({
      node,
      canFindMiners,
      onFindMiners,
      onDismiss,
    }: {
      node: FleetNodeItem;
      canFindMiners: boolean;
      onFindMiners: () => void;
      onDismiss: () => void;
    }) {
      return (
        <div role="dialog" aria-label="Node details">
          {node.name}
          {canFindMiners ? (
            <button type="button" onClick={onFindMiners}>
              Find miners
            </button>
          ) : null}
          <button type="button" onClick={onDismiss}>
            Close details
          </button>
        </div>
      );
    },
  };
});

vi.mock("@/protoFleet/features/settings/components/Nodes/RevokeNodeDialog", () => ({
  default: ({
    open,
    nodeName,
    affectedDeviceTypes,
    isLoadingImpact,
    impactError,
    onConfirm,
  }: {
    open: boolean;
    nodeName: string;
    affectedDeviceTypes: string[];
    isLoadingImpact: boolean;
    impactError: string;
    onConfirm: () => void;
    onDismiss: () => void;
    isSubmitting: boolean;
  }) =>
    open ? (
      <div role="dialog" aria-label="Revoke node">
        <div>{nodeName}</div>
        <div>{affectedDeviceTypes.join(", ")}</div>
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
    listFleetNodeDiscoveredDevicesMock.mockReset();
    listFleetNodeDiscoveredDevicesMock.mockResolvedValue({ devices: [], nextCursor: 0n });
    discoverOnFleetNodeMock.mockReset();
    discoverOnFleetNodeMock.mockResolvedValue(undefined);
    pairDiscoveredDevicesOnFleetNodeMock.mockReset();
    pairDiscoveredDevicesOnFleetNodeMock.mockResolvedValue(undefined);
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

  it("opens Node details without a redundant status summary", async () => {
    listFleetNodesMock.mockResolvedValue([{ ...confirmedNode, lastSeenAt: new Date() }]);
    renderNodesPage();

    expect(await screen.findByText("node-01")).toBeInTheDocument();
    expect(screen.queryByLabelText("Node status totals")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    expect(screen.getByRole("dialog", { name: "Node details" })).toHaveTextContent("node-01");
  });

  it("opens the selected Node's finder from its details", async () => {
    permissionsMock.current = ["fleetnode:read", "fleetnode:manage", "miner:pair"];
    renderNodesPage();

    await screen.findByText("node-01");
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    expect(screen.queryByRole("dialog", { name: "Node details" })).not.toBeInTheDocument();
    expect(screen.getByTestId("find-node-miners-modal")).toHaveTextContent("node-01");
  });

  it("keeps the reopened finder intact when an earlier pairing settles", async () => {
    permissionsMock.current = ["fleetnode:read", "fleetnode:manage", "miner:pair"];
    listFleetNodeDiscoveredDevicesMock.mockResolvedValue({
      devices: [
        create(FleetNodeDiscoveredDeviceSchema, { deviceIdentifier: "miner-2", ipAddress: "192.168.1.2" }),
        create(FleetNodeDiscoveredDeviceSchema, { deviceIdentifier: "miner-3", ipAddress: "192.168.1.3" }),
        create(FleetNodeDiscoveredDeviceSchema, { deviceIdentifier: "miner-4", ipAddress: "192.168.1.4" }),
      ],
      nextCursor: 0n,
    });
    let finishFirstPairing: (() => void) | undefined;
    pairDiscoveredDevicesOnFleetNodeMock.mockImplementationOnce(
      (_nodeId, _identifiers, _credentials, onResults) =>
        new Promise<void>((_resolve, reject) => {
          finishFirstPairing = () => {
            onResults([
              create(DevicePairingResultSchema, {
                deviceIdentifier: "miner-2",
                pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
              }),
            ]);
            reject(new Error("Pair result stream disconnected"));
          };
        }),
    );
    renderNodesPage();

    await screen.findByText("node-01");
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-3" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Add selected (2)" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Add selected (2)" }));
    await waitFor(() => expect(pairDiscoveredDevicesOnFleetNodeMock).toHaveBeenCalledTimes(1));

    fireEvent.click(within(screen.getByTestId("find-node-miners-modal")).getByRole("button", { name: "Close dialog" }));
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    const reopened = screen.getByTestId("find-node-miners-modal");
    expect(await screen.findByRole("checkbox", { name: "Select miner-2" })).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "Select miner-3" })).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-4" }));
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "operator" } });

    await act(async () => finishFirstPairing?.());

    expect(screen.getByTestId("find-node-miners-modal")).toBe(reopened);
    expect(screen.getByRole("checkbox", { name: "Select miner-4" })).toBeChecked();
    expect(screen.getByLabelText("Username")).toHaveValue("operator");
    expect(screen.getByRole("checkbox", { name: "Select miner-2" })).toBeEnabled();
    expect(screen.getByRole("checkbox", { name: "Select miner-3" })).toBeDisabled();
  });

  it("loads the affected miners before allowing revocation", async () => {
    listFleetNodeDevicesMock.mockResolvedValue([
      { deviceIdentifier: "miner-1", deviceType: "proto" },
      { deviceIdentifier: "miner-2", deviceType: "antminer" },
    ]);
    renderNodesPage();

    expect(await screen.findByText("node-01")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: "Revoke node" })).toHaveTextContent("proto, antminer"),
    );
    expect(screen.getByRole("dialog", { name: "Revoke node" })).not.toHaveTextContent("miner-1");
    expect(listFleetNodeDevicesMock).toHaveBeenCalledWith("7");
  });
});
