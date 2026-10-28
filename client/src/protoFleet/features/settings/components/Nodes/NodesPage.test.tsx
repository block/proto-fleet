import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import NodesPage from "./NodesPage";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DevicePairingResultSchema,
  FleetNodeDiscoveredDeviceSchema,
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
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
      onRevoke,
    }: {
      node: FleetNodeItem;
      canFindMiners: boolean;
      onFindMiners: () => void;
      onDismiss: () => void;
      onRevoke?: () => void;
    }) {
      return (
        <div role="dialog" aria-label="Node details">
          {node.name}
          {canFindMiners ? (
            <button type="button" onClick={onFindMiners}>
              Find miners
            </button>
          ) : null}
          {onRevoke ? (
            <button type="button" onClick={onRevoke}>
              Revoke
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
  discoveryCoverageDegraded: false,
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

const openNodeRevoke = async (name = "node-01") => {
  fireEvent.click(screen.getByRole("button", { name: `View ${name}` }));
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
  });
};

describe("NodesPage", () => {
  it("shows effective discovery ranges only while connected", async () => {
    permissionsMock.current = ["fleetnode:read"];
    listFleetNodesMock.mockResolvedValue([
      {
        ...confirmedNode,
        controlStreamConnected: true,
        discoveryConfigReported: true,
        discoveryRanges: ["10.0.0.1-10.0.15.254"],
      },
    ]);
    renderNodesPage();
    expect(await screen.findByText("10.0.0.1-10.0.15.254")).toBeVisible();
  });
  afterEach(() => vi.useRealTimers());
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
    expect(screen.queryByRole("button", { name: "Set up a node" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Confirm enrollment" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View node-01" }));
    expect(screen.queryByRole("button", { name: "Revoke" })).not.toBeInTheDocument();
  });

  it("shows each HA member's role separately from its connection", async () => {
    listFleetNodesMock.mockResolvedValue([
      {
        ...confirmedNode,
        haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE,
        haPeerNodeId: "8",
        haPeerNodeName: "standby-node",
      },
      {
        ...confirmedNode,
        fleetNodeId: "8",
        name: "standby-node",
        haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
        haPeerNodeId: "7",
        haPeerNodeName: "node-01",
        haStatusReason: "Waiting for both nodes to connect",
      },
    ]);

    renderNodesPage();

    expect(await screen.findByText("standby-node")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByText("Preparing")).toBeInTheDocument();
    expect(screen.getAllByTitle("Waiting for both nodes to connect")).not.toHaveLength(0);
  });

  it.each([
    { ipAddresses: ["192.168.1.7"], expected: "192.168.1.7", version: "v0.5.1-test" },
    { ipAddresses: ["192.168.1.7", "fd00::7"], expected: "192.168.1.7 +1", version: "v0.5.1-test" },
    { ipAddresses: [], expected: "—", version: "" },
  ])("renders host metadata for $expected", async ({ ipAddresses, expected, version }) => {
    listFleetNodesMock.mockResolvedValue([{ ...confirmedNode, ipAddresses, version }]);

    renderNodesPage();

    await screen.findByText("node-01");
    expect(screen.getByText("IP address")).toBeVisible();
    expect(screen.getByText("Version")).toBeVisible();
    if (ipAddresses.length) {
      expect(screen.getByTitle(ipAddresses.join(", "))).toHaveTextContent(expected);
      expect(screen.getByText(version)).toBeVisible();
    } else {
      expect(screen.getAllByText("—")).toHaveLength(2);
    }
  });

  it("shows management actions for users with fleetnode manage permission", async () => {
    listFleetNodesMock.mockResolvedValue([awaitingNode]);

    renderNodesPage();

    expect(await screen.findByText("node-pending")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Set up a node" }));
    expect(screen.getByTestId("enroll-node-modal")).toHaveTextContent("New enrollment");

    fireEvent.click(screen.getByRole("button", { name: "Confirm enrollment" }));
    expect(screen.getByTestId("enroll-node-modal")).toHaveTextContent("Resume node-pending");
  });

  it("passes the pending enrollment id when revoking an awaiting node", async () => {
    listFleetNodesMock.mockResolvedValue([awaitingNode]);

    renderNodesPage();

    expect(await screen.findByText("node-pending")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Revoke" })).not.toBeInTheDocument();
    await openNodeRevoke("node-pending");
    expect(screen.queryByRole("dialog", { name: "Node details" })).not.toBeInTheDocument();
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
    await openNodeRevoke();
    expect(await screen.findByText("1 paired Bitmain S19 miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired MicroBT M60 miner will lose connection.")).toBeInTheDocument();
    expect(screen.queryByText(/ASIC-RS|miner-1/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revoke node" })).toBeEnabled();
    expect(listFleetNodeDevicesMock).toHaveBeenCalledWith("7");
  });

  const activeNode: FleetNodeItem = {
    ...confirmedNode,
    haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE,
    haPeerNodeId: "8",
    haPeerNodeName: "standby-node",
    haStandbyReady: true,
  };
  const standbyNode: FleetNodeItem = {
    ...confirmedNode,
    fleetNodeId: "8",
    name: "standby-node",
    haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
    haPeerNodeId: "7",
    haPeerNodeName: "node-01",
    haStandbyReady: true,
  };

  it("keeps HA pairs adjacent and in the same order after failover and polling reorder the nodes", async () => {
    vi.useFakeTimers();
    const standalone = { ...confirmedNode, fleetNodeId: "9", name: "other-node" };
    listFleetNodesMock.mockResolvedValue([standbyNode, standalone, activeNode]);
    await act(async () => {
      renderNodesPage();
    });
    const rowNames = () => screen.getAllByRole("button", { name: /^View / }).map((button) => button.textContent);
    expect(rowNames()).toEqual(["View node-01", "View standby-node", "View other-node"]);

    listFleetNodesMock.mockResolvedValue([
      { ...activeNode, haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY },
      standalone,
      { ...standbyNode, haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE },
    ]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(rowNames()).toEqual(["View node-01", "View standby-node", "View other-node"]);
  });

  it("explains that a ready standby takes over when revoking the active node", async () => {
    listFleetNodesMock.mockResolvedValue([activeNode, standbyNode]);
    renderNodesPage();
    await screen.findByText("standby-node");

    await openNodeRevoke();

    expect(await screen.findByText(/"node-01" will lose access to Fleet/)).toBeInTheDocument();
    expect(screen.getByText(/standby-node will take over its miners/)).toBeInTheDocument();
  });

  it.each([
    ["role", [{ ...activeNode, haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY }, standbyNode]],
    ["peer", [{ ...activeNode, haPeerNodeId: "9" }, standbyNode]],
    ["readiness", [{ ...activeNode, haStandbyReady: false }, standbyNode]],
    ["device ownership", [{ ...activeNode, pairedDeviceCount: 1 }, standbyNode]],
    ["peer connection", [activeNode, { ...standbyNode, controlStreamConnected: false }]],
    ["removed target", [standbyNode]],
  ])("requires reopening revoke after polling changes %s", async (_change, updatedNodes) => {
    vi.useFakeTimers();
    listFleetNodesMock.mockResolvedValue([activeNode, standbyNode]);
    await act(async () => {
      renderNodesPage();
    });
    await openNodeRevoke();
    expect(screen.getByRole("button", { name: "Revoke node" })).toBeEnabled();

    listFleetNodesMock.mockResolvedValue(updatedNodes);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });

    expect(listFleetNodesMock).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("button", { name: "Revoke node" })).not.toBeInTheDocument();
    expect(revokeFleetNodeMock).not.toHaveBeenCalled();
  });

  it("keeps the confirmation open when a poll only updates host metadata", async () => {
    vi.useFakeTimers();
    listFleetNodesMock.mockResolvedValue([activeNode, standbyNode]);
    await act(async () => {
      renderNodesPage();
    });
    await openNodeRevoke();

    listFleetNodesMock.mockResolvedValue([
      { ...activeNode, version: "new-version", lastSeenAt: new Date() },
      standbyNode,
    ]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });

    expect(listFleetNodesMock).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("button", { name: "Revoke node" })).toBeEnabled();
  });

  it("reviews the promoted standby's current impact before allowing revocation", async () => {
    vi.useFakeTimers();
    listFleetNodesMock.mockResolvedValue([activeNode, standbyNode]);
    await act(async () => {
      renderNodesPage();
    });
    await openNodeRevoke("standby-node");
    expect(screen.getByText(/node-01 will keep managing its miners/)).toBeInTheDocument();

    listFleetNodesMock.mockResolvedValue([
      {
        ...standbyNode,
        haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
        haPeerNodeId: null,
        haPeerNodeName: "",
        pairedDeviceCount: 1,
      },
    ]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(screen.queryByRole("button", { name: "Revoke node" })).not.toBeInTheDocument();

    listFleetNodeDevicesMock.mockResolvedValue([{ deviceIdentifier: "miner-1", manufacturer: "Proto", model: "Rig" }]);
    await openNodeRevoke("standby-node");
    expect(screen.getByText("1 paired Proto Rig miner will lose connection.")).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Revoke node" }));
    });
    expect(revokeFleetNodeMock).toHaveBeenCalledWith("8", null);
  });

  it("ignores an obsolete impact response after polling invalidates and reopens revoke", async () => {
    vi.useFakeTimers();
    let finishOldImpact: ((devices: unknown[]) => void) | undefined;
    let finishCurrentImpact: ((devices: unknown[]) => void) | undefined;
    listFleetNodeDevicesMock
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishOldImpact = resolve;
          }),
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishCurrentImpact = resolve;
          }),
      );
    await act(async () => {
      renderNodesPage();
    });
    await openNodeRevoke();
    expect(screen.getByRole("button", { name: "Revoke node" })).toBeDisabled();

    listFleetNodesMock.mockResolvedValue([{ ...confirmedNode, pairedDeviceCount: 1 }]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(screen.queryByRole("button", { name: "Revoke node" })).not.toBeInTheDocument();
    await openNodeRevoke();
    await act(async () => {
      finishOldImpact?.([{ manufacturer: "Old", model: "Miner" }]);
    });
    expect(screen.getByRole("button", { name: "Revoke node" })).toBeDisabled();
    expect(screen.queryByText(/paired Old Miner/)).not.toBeInTheDocument();

    await act(async () => {
      finishCurrentImpact?.([{ manufacturer: "Proto", model: "Rig" }]);
    });
    expect(screen.getByRole("button", { name: "Revoke node" })).toBeEnabled();
    expect(screen.getByText("1 paired Proto Rig miner will lose connection.")).toBeInTheDocument();
    expect(revokeFleetNodeMock).not.toHaveBeenCalled();
  });

  it("allows retry after authentication is revoked but handoff fails", async () => {
    vi.useFakeTimers();
    listFleetNodesMock.mockResolvedValue([activeNode, standbyNode]);
    revokeFleetNodeMock.mockRejectedValueOnce(new Error("Handoff interrupted"));
    await act(async () => {
      renderNodesPage();
    });
    await openNodeRevoke();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Revoke node" }));
    });

    listFleetNodesMock.mockResolvedValue([
      { ...activeNode, enrollmentStatus: FleetNodeEnrollmentStatus.REVOKED, controlStreamConnected: false },
      standbyNode,
    ]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });

    expect(screen.getByRole("button", { name: "Revoke node" })).toBeEnabled();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Revoke node" }));
    });
    expect(revokeFleetNodeMock).toHaveBeenCalledTimes(2);
    expect(revokeFleetNodeMock).toHaveBeenLastCalledWith("7", null);
  });

  it("warns that the standby cannot take over when its control stream is down", async () => {
    listFleetNodesMock.mockResolvedValue([activeNode, { ...standbyNode, controlStreamConnected: false }]);
    renderNodesPage();
    await screen.findByText("standby-node");

    await openNodeRevoke();

    expect(
      await screen.findByText(/Its miners will keep running, but Fleet will lose access to them/),
    ).toBeInTheDocument();
  });
});
