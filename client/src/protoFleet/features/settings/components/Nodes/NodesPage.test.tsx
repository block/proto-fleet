import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
import type { ColConfig, ColTitles, ListAction, ListActionValue } from "@/shared/components/List/types";

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
      activeCols,
      colConfig,
      colTitles,
    }: {
      items: FleetNodeItem[];
      noDataElement?: ReactNode;
      actions?: ListAction<FleetNodeItem>[];
      onRowClick?: (node: FleetNodeItem) => void;
      activeCols: string[];
      colConfig: ColConfig<FleetNodeItem, string, string>;
      colTitles: ColTitles<string>;
    }) => (
      <div data-testid="nodes-list">
        {activeCols.map((column) => (
          <span key={column}>{colTitles[column]}</span>
        ))}
        {items.length === 0
          ? noDataElement
          : items.map((node) => (
              <div key={node.fleetNodeId}>
                {activeCols.map((column) => (
                  <div key={column}>{colConfig[column]?.component?.(node, [])}</div>
                ))}
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
  commandProtocolUpgradeRecommended: false,
  controlStreamConnected: true,
  pairedDeviceCount: 0,
  ipAddresses: [],
  version: "",
  enrolledBy: "",
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

  it.each([
    { ipAddresses: ["192.168.1.7"], expected: "192.168.1.7", version: "v0.5.1-test" },
    { ipAddresses: ["192.168.1.7", "fd00::7"], expected: "192.168.1.7 +1", version: "v0.5.1-test" },
    { ipAddresses: [], expected: "Unavailable", version: "" },
  ])("renders host metadata for $expected", async ({ ipAddresses, expected, version }) => {
    // Arrange
    listFleetNodesMock.mockResolvedValue([{ ...confirmedNode, ipAddresses, version }]);

    // Act
    renderNodesPage();
    await screen.findByText("node-01");

    // Assert
    expect(screen.getByText("IP address")).toBeVisible();
    expect(screen.getByText("Version")).toBeVisible();
    if (ipAddresses.length) {
      expect(screen.getByTitle(ipAddresses.join(", "))).toHaveTextContent(expected);
      expect(screen.getByText(version)).toBeVisible();
    } else {
      expect(screen.getAllByText("Unavailable")).toHaveLength(2);
    }
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
    expect(screen.getByText("Revoke node?")).toBeInTheDocument();

    await waitFor(() => expect(screen.getByRole("button", { name: "Revoke node" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Revoke node" }));

    await waitFor(() => expect(revokeFleetNodeMock).toHaveBeenCalledWith("7", "11"));
  });

  it("opens the selected Node's finder from its details", async () => {
    permissionsMock.current = ["fleetnode:read", "fleetnode:manage", "miner:pair"];
    renderNodesPage();

    await screen.findByText("node-01");
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    expect(screen.queryByRole("dialog", { name: "Node details" })).not.toBeInTheDocument();
    expect(screen.getByText("Add miners")).toBeInTheDocument();
    await waitFor(() =>
      expect(discoverOnFleetNodeMock).toHaveBeenCalledWith(
        "7",
        expect.anything(),
        expect.anything(),
        expect.any(AbortSignal),
      ),
    );
  });

  it("reattaches to a running Node scan after closing and reopening the finder", async () => {
    permissionsMock.current = ["fleetnode:read", "fleetnode:manage", "miner:pair"];
    let finishScan: (() => void) | undefined;
    let scanFinished = false;
    discoverOnFleetNodeMock.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finishScan = () => {
            scanFinished = true;
            resolve();
          };
        }),
    );
    listFleetNodeDiscoveredDevicesMock.mockImplementation(async () => ({
      devices: scanFinished
        ? [create(FleetNodeDiscoveredDeviceSchema, { deviceIdentifier: "miner-2", ipAddress: "192.168.1.2" })]
        : [],
      nextCursor: 0n,
    }));
    renderNodesPage();

    await screen.findByText("node-01");
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    await waitFor(() => expect(discoverOnFleetNodeMock).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("button", { name: "Close add miners" }));
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    await waitFor(() => expect(listFleetNodeDiscoveredDevicesMock).toHaveBeenCalledTimes(2));
    expect(discoverOnFleetNodeMock).toHaveBeenCalledTimes(1);

    await act(async () => finishScan?.());

    expect(await screen.findByRole("button", { name: "Continue with 1 miners" })).toBeInTheDocument();
    expect(discoverOnFleetNodeMock).toHaveBeenCalledTimes(1);
  });

  it("aborts running Node scans when the page unmounts", async () => {
    permissionsMock.current = ["fleetnode:read", "fleetnode:manage", "miner:pair"];
    discoverOnFleetNodeMock.mockImplementation(() => new Promise<void>(() => {}));
    const { unmount } = renderNodesPage();

    await screen.findByText("node-01");
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    await waitFor(() => expect(discoverOnFleetNodeMock).toHaveBeenCalledTimes(1));
    const signal = discoverOnFleetNodeMock.mock.calls[0][3] as AbortSignal;
    expect(signal.aborted).toBe(false);

    unmount();

    expect(signal.aborted).toBe(true);
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
    fireEvent.click(await screen.findByRole("button", { name: "Continue with 3 miners" }));
    await waitFor(() => expect(pairDiscoveredDevicesOnFleetNodeMock).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("button", { name: "Close add miners" }));
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    fireEvent.click(screen.getByRole("button", { name: "Find miners" }));
    expect(screen.getByText("Add miners")).toBeInTheDocument();
    expect(await screen.findByText(/3 miners have an unknown pairing result/)).toBeInTheDocument();

    await act(async () => finishFirstPairing?.());

    expect(screen.getByText("Add miners")).toBeInTheDocument();
    expect(await screen.findByText(/2 miners have an unknown pairing result/)).toBeInTheDocument();
    await waitFor(() => expect(listFleetNodeDiscoveredDevicesMock).toHaveBeenCalled());
  });

  it("loads the affected miners before allowing revocation", async () => {
    listFleetNodeDevicesMock.mockResolvedValue([
      { deviceIdentifier: "miner-1", deviceType: "asicrs", manufacturer: "Bitmain", model: "S19" },
      { deviceIdentifier: "miner-2", deviceType: "asicrs", manufacturer: "MicroBT", model: "M60" },
    ]);
    renderNodesPage();

    expect(await screen.findByText("node-01")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    expect(await screen.findByText("1 paired Bitmain S19 miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired MicroBT M60 miner will lose connection.")).toBeInTheDocument();
    expect(screen.queryByText(/ASIC-RS|miner-1/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revoke node" })).toBeEnabled();
    expect(listFleetNodeDevicesMock).toHaveBeenCalledWith("7");
  });
});
