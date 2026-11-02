import type { ComponentProps } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import FindNodeMinersModal from "./FindNodeMinersModal";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DevicePairingResultSchema,
  FleetNodeDiscoveredDeviceSchema,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import {
  FleetNodeEnrollmentStatus,
  FleetNodeHARole,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { createErrorWithCause } from "@/protoFleet/api/requestErrors";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetStore } from "@/protoFleet/store";

const listDiscovered = vi.hoisted(() => vi.fn());
const discoverOnNode = vi.hoisted(() => vi.fn());
const pairOnNode = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useFleetNodes", () => ({
  useFleetNodes: () => ({
    listFleetNodeDiscoveredDevices: listDiscovered,
    discoverOnFleetNode: discoverOnNode,
    pairDiscoveredDevicesOnFleetNode: pairOnNode,
  }),
}));

const node: FleetNodeItem = {
  fleetNodeId: "7",
  pendingEnrollmentId: null,
  name: "node-01",
  enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
  identityFingerprint: "abcd1234abcd1234",
  commandProtocolUpgradeRequired: false,
  commandProtocolUpgradeRecommended: false,
  controlStreamConnected: true,
  discoveryCoverageDegraded: false,
  pairedDeviceCount: 1,
  haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
  haPeerNodeId: null,
  haPeerNodeName: "",
  haStandbyReady: false,
  haStatusReason: "",
  ipAddresses: [],
  version: "",
  enrolledBy: "",
  createdAt: new Date("2026-07-09T12:00:00Z"),
  lastSeenAt: new Date(),
};

const miner = create(FleetNodeDiscoveredDeviceSchema, {
  fleetNodeId: 7n,
  deviceIdentifier: "miner-2",
  ipAddress: "192.168.1.2",
  model: "Proto Rig",
});
const otherMiner = create(FleetNodeDiscoveredDeviceSchema, {
  fleetNodeId: 7n,
  deviceIdentifier: "miner-3",
  ipAddress: "192.168.1.3",
});

const renderFinder = (props: Partial<ComponentProps<typeof FindNodeMinersModal>> = {}) =>
  render(
    <FindNodeMinersModal
      node={node}
      canPair
      blockedPairingIdentifiers={[]}
      settledPairingRevision={0}
      onDismiss={vi.fn()}
      onUpdated={vi.fn()}
      onPairingStarted={vi.fn()}
      onPairingCompleted={vi.fn()}
      onPairingSettledAfterDismiss={vi.fn()}
      scanOnNode={discoverOnNode}
      {...props}
    />,
  );

beforeEach(() => {
  useFleetStore.getState().auth.setPermissions([]);
  listDiscovered.mockReset().mockResolvedValue({ devices: [miner], nextCursor: 0n });
  discoverOnNode.mockReset().mockResolvedValue(undefined);
  pairOnNode.mockReset().mockResolvedValue(undefined);
});

describe("FindNodeMinersModal", () => {
  it("scans this Node's local subnet even when an upgrade is recommended", async () => {
    renderFinder({ node: { ...node, commandProtocolUpgradeRecommended: true } });

    await waitFor(() => expect(discoverOnNode).toHaveBeenCalledTimes(1));
    expect(discoverOnNode.mock.calls[0][0]).toBe("7");
    expect(discoverOnNode.mock.calls[0][1].mode).toMatchObject({
      case: "networkScan",
      value: { useFleetNodeLocalSubnet: true, target: "" },
    });
    expect(await screen.findByRole("button", { name: "Continue with 1 miners" })).toBeInTheDocument();
  });

  it("lets operators without pairing permission scan without add controls", async () => {
    renderFinder({ canPair: false });

    expect(await screen.findByText(/1 miner found on your network/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Continue with/ })).not.toBeInTheDocument();
    expect(screen.queryByText("Add found miners")).not.toBeInTheDocument();
  });

  it("rescans this Node and refreshes its discoveries", async () => {
    listDiscovered
      .mockResolvedValueOnce({ devices: [miner], nextCursor: 0n })
      .mockResolvedValueOnce({ devices: [miner], nextCursor: 0n })
      .mockResolvedValueOnce({ devices: [otherMiner], nextCursor: 0n });
    renderFinder();

    await screen.findByRole("button", { name: "Continue with 1 miners" });
    fireEvent.click(screen.getByRole("button", { name: "Rescan network" }));

    await waitFor(() => expect(listDiscovered).toHaveBeenCalledTimes(3));
    fireEvent.click(screen.getByRole("button", { name: "Continue with 1 miners" }));
    expect(pairOnNode).toHaveBeenCalledWith("7", ["miner-3"], undefined, expect.any(Function));
    expect(discoverOnNode).toHaveBeenCalledTimes(2);
  });

  it("loads miners saved by a scan even when its stream fails", async () => {
    listDiscovered
      .mockResolvedValueOnce({ devices: [], nextCursor: 0n })
      .mockResolvedValueOnce({ devices: [miner], nextCursor: 0n });
    discoverOnNode.mockRejectedValue(new Error("Scan disconnected"));
    renderFinder();

    expect(await screen.findByRole("button", { name: "Continue with 1 miners" })).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Scan disconnected");
  });

  it("pairs only miners chosen in the shared selection dialog", async () => {
    listDiscovered.mockResolvedValue({ devices: [miner, otherMiner], nextCursor: 0n });
    renderFinder();

    fireEvent.click(await screen.findByRole("button", { name: "Choose miners" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-3" }));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    fireEvent.click(screen.getByRole("button", { name: "Continue with 1 miners" }));

    await waitFor(() => expect(pairOnNode).toHaveBeenCalledWith("7", ["miner-2"], undefined, expect.any(Function)));
  });

  it("releases reported miners but keeps unreported miners blocked after a stream failure", async () => {
    listDiscovered.mockResolvedValue({ devices: [miner, otherMiner], nextCursor: 0n });
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, {
          deviceIdentifier: "miner-2",
          pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
        }),
      ]);
      throw new Error("Pair result stream disconnected");
    });
    const onPairingStarted = vi.fn();
    const onPairingCompleted = vi.fn();
    renderFinder({ onPairingStarted, onPairingCompleted });

    fireEvent.click(await screen.findByRole("button", { name: "Continue with 2 miners" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Pair result stream disconnected");
    expect(onPairingStarted).toHaveBeenCalledWith(["miner-2", "miner-3"]);
    expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]);
    expect(onPairingCompleted).toHaveBeenCalledTimes(1);
  });

  it("dismisses after every selected miner is reported paired", async () => {
    listDiscovered.mockResolvedValue({ devices: [miner, otherMiner], nextCursor: 0n });
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, { deviceIdentifier: "miner-2", pairingStatus: PairingStatus.PAIRED }),
        create(DevicePairingResultSchema, { deviceIdentifier: "miner-3", pairingStatus: PairingStatus.PAIRED }),
      ]);
    });
    const onDismiss = vi.fn();
    const onPairingCompleted = vi.fn();
    renderFinder({ onDismiss, onPairingCompleted });

    fireEvent.click(await screen.findByRole("button", { name: "Continue with 2 miners" }));

    await waitFor(() => expect(onDismiss).toHaveBeenCalledTimes(1));
    expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2", "miner-3"]);
    expect(onPairingCompleted).toHaveBeenCalledTimes(1);
  });

  it("keeps the finder open when a completed stream has authentication-needed and unreported miners", async () => {
    listDiscovered.mockResolvedValue({ devices: [miner, otherMiner], nextCursor: 0n });
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, {
          deviceIdentifier: "miner-2",
          pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
        }),
      ]);
    });
    const onDismiss = vi.fn();
    const onPairingCompleted = vi.fn();
    renderFinder({ onDismiss, onPairingCompleted });

    fireEvent.click(await screen.findByRole("button", { name: "Continue with 2 miners" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Some pairing results need attention");
    expect(onDismiss).not.toHaveBeenCalled();
    expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]);
    expect(onPairingCompleted).toHaveBeenCalledTimes(1);
  });

  it("keeps synthetic FAILED pairing results blocked after a stream failure", async () => {
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, {
          deviceIdentifier: "miner-2",
          pairingStatus: PairingStatus.FAILED,
          error: "device was not paired before the batch completed (timed out or truncated); retry",
        }),
      ]);
      throw new Error("Node control stream disconnected");
    });
    const onPairingStarted = vi.fn();
    const onPairingCompleted = vi.fn();
    renderFinder({ onPairingStarted, onPairingCompleted });

    fireEvent.click(await screen.findByRole("button", { name: "Continue with 1 miners" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Node control stream disconnected");
    expect(onPairingStarted).toHaveBeenCalledWith(["miner-2"]);
    expect(onPairingCompleted).not.toHaveBeenCalled();
  });

  it("keeps later pages visible after a late pairing result refresh", async () => {
    const refreshedMiner = create(FleetNodeDiscoveredDeviceSchema, {
      fleetNodeId: 7n,
      deviceIdentifier: "miner-4",
      ipAddress: "192.168.1.4",
      model: "Proto Rig",
    });
    let refreshed = false;
    const refreshedPageLoaded = vi.fn();
    listDiscovered.mockImplementation(async (_nodeId, cursor = 0n) => {
      if (cursor === 0n) return { devices: [miner], nextCursor: 42n };
      if (refreshed) {
        refreshedPageLoaded();
        return { devices: [refreshedMiner], nextCursor: 0n };
      }
      return { devices: [otherMiner], nextCursor: 0n };
    });
    const props = {
      node,
      canPair: true,
      blockedPairingIdentifiers: [],
      onDismiss: vi.fn(),
      onUpdated: vi.fn(),
      onPairingStarted: vi.fn(),
      onPairingCompleted: vi.fn(),
      onPairingSettledAfterDismiss: vi.fn(),
      scanOnNode: discoverOnNode,
    };
    const { rerender } = render(<FindNodeMinersModal {...props} settledPairingRevision={0} />);

    await screen.findByRole("button", { name: "Continue with 1 miners" });
    fireEvent.click(screen.getByRole("button", { name: "Load next 100" }));
    expect(await screen.findByRole("button", { name: "Continue with 2 miners" })).toBeInTheDocument();
    refreshed = true;
    rerender(<FindNodeMinersModal {...props} settledPairingRevision={1} />);

    await waitFor(() => expect(refreshedPageLoaded).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: "Choose miners" }));
    expect(await screen.findByText("192.168.1.4")).toBeInTheDocument();
    expect(screen.queryByText("192.168.1.3")).not.toBeInTheDocument();
  });

  it("releases retry blocks after definitive pre-dispatch rejection", async () => {
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, {
          deviceIdentifier: "miner-2",
          pairingStatus: PairingStatus.FAILED,
        }),
      ]);
      throw createErrorWithCause(
        "fleet node has no active control stream",
        new ConnectError("fleet node has no active control stream", Code.FailedPrecondition),
      );
    });
    const onPairingCompleted = vi.fn();
    renderFinder({ onPairingCompleted });

    fireEvent.click(await screen.findByRole("button", { name: "Continue with 1 miners" }));

    await waitFor(() => expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]));
  });

  it("releases retry blocks when the Node rejects a busy pairing command", async () => {
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, {
          deviceIdentifier: "miner-2",
          pairingStatus: PairingStatus.FAILED,
        }),
      ]);
      throw createErrorWithCause(
        "fleet node is busy with another command; retry shortly: busy",
        new ConnectError("fleet node is busy with another command; retry shortly: busy", Code.ResourceExhausted),
      );
    });
    const onPairingCompleted = vi.fn();
    renderFinder({ onPairingCompleted });

    fireEvent.click(await screen.findByRole("button", { name: "Continue with 1 miners" }));

    await waitFor(() => expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]));
  });

  it("keeps the original pairing error and result when refreshing fails", async () => {
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, { deviceIdentifier: "miner-2", pairingStatus: PairingStatus.PAIRED }),
      ]);
      throw new Error("Pair result stream disconnected");
    });
    const onPairingCompleted = vi.fn();
    renderFinder({ onPairingCompleted });

    await screen.findByRole("button", { name: "Continue with 1 miners" });
    listDiscovered.mockRejectedValueOnce(new Error("Refresh failed"));
    fireEvent.click(screen.getByRole("button", { name: "Continue with 1 miners" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Pair result stream disconnected");
    expect(screen.getByText("miner-2: PAIRED")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Continue with 1 miners" })).not.toBeInTheDocument();
    expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]);
  });
});
