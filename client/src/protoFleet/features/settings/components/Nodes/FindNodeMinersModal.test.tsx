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
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { createErrorWithCause } from "@/protoFleet/api/requestErrors";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

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
  controlStreamConnected: true,
  pairedDeviceCount: 1,
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
      {...props}
    />,
  );

beforeEach(() => {
  listDiscovered.mockReset().mockResolvedValue({ devices: [miner], nextCursor: 0n });
  discoverOnNode.mockReset().mockResolvedValue(undefined);
  pairOnNode.mockReset().mockResolvedValue(undefined);
});

describe("FindNodeMinersModal", () => {
  it("automatically scans only this Node's local subnet", async () => {
    renderFinder();

    await waitFor(() => expect(discoverOnNode).toHaveBeenCalledTimes(1));
    expect(discoverOnNode.mock.calls[0][0]).toBe("7");
    expect(discoverOnNode.mock.calls[0][1].mode).toMatchObject({
      case: "networkScan",
      value: { useFleetNodeLocalSubnet: true, target: "" },
    });
    expect(await screen.findByRole("checkbox", { name: "Select miner-2" })).toBeInTheDocument();
    expect(screen.queryByText(/Paired miners/)).not.toBeInTheDocument();
    expect(screen.queryByText("Pair selected miners")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Scan target")).not.toBeInTheDocument();
  });

  it("lets operators without pairing permission scan without add controls", async () => {
    renderFinder({ canPair: false });

    expect(await screen.findByText(/miner-2/)).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Select miner-2" })).not.toBeInTheDocument();
    expect(screen.queryByText("Add found miners")).not.toBeInTheDocument();
  });

  it("rescans this Node and refreshes its discoveries", async () => {
    listDiscovered
      .mockResolvedValueOnce({ devices: [miner], nextCursor: 0n })
      .mockResolvedValueOnce({ devices: [miner], nextCursor: 0n })
      .mockResolvedValueOnce({ devices: [otherMiner], nextCursor: 0n });
    renderFinder();

    await screen.findByRole("checkbox", { name: "Select miner-2" });
    fireEvent.click(screen.getByRole("button", { name: "Scan again" }));

    expect(await screen.findByRole("checkbox", { name: "Select miner-3" })).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Select miner-2" })).not.toBeInTheDocument();
    expect(discoverOnNode).toHaveBeenCalledTimes(2);
  });

  it("shows persisted discoveries when a scan is rejected because the Node is busy", async () => {
    discoverOnNode.mockRejectedValue(new Error("Node is busy"));
    renderFinder();

    expect(await screen.findByRole("checkbox", { name: "Select miner-2" })).toBeInTheDocument();
    expect(await screen.findByRole("alert")).toHaveTextContent("Node is busy");
  });

  it("loads miners saved by a scan even when its stream fails", async () => {
    listDiscovered
      .mockResolvedValueOnce({ devices: [], nextCursor: 0n })
      .mockResolvedValueOnce({ devices: [miner], nextCursor: 0n });
    discoverOnNode.mockRejectedValue(new Error("Scan disconnected"));
    renderFinder();

    expect(await screen.findByRole("checkbox", { name: "Select miner-2" })).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Scan disconnected");
  });

  it("adds selected miners through this Node and shows the result", async () => {
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, { deviceIdentifier: "miner-2", pairingStatus: PairingStatus.PAIRED }),
      ]);
    });
    const onUpdated = vi.fn();
    const onPairingCompleted = vi.fn();
    renderFinder({ onUpdated, onPairingCompleted });

    fireEvent.click(await screen.findByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("button", { name: "Add selected (1)" }));

    await waitFor(() => expect(pairOnNode).toHaveBeenCalledTimes(1));
    expect(pairOnNode.mock.calls[0][0]).toBe("7");
    expect(pairOnNode.mock.calls[0][1]).toEqual(["miner-2"]);
    expect(await screen.findByText("miner-2: PAIRED")).toBeInTheDocument();
    await waitFor(() => expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]));
    expect(onUpdated).toHaveBeenCalled();
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

    fireEvent.click(await screen.findByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-3" }));
    fireEvent.click(screen.getByRole("button", { name: "Add selected (2)" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Pair result stream disconnected");
    expect(onPairingStarted).toHaveBeenCalledWith(["miner-2", "miner-3"]);
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

    fireEvent.click(await screen.findByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("button", { name: "Add selected (1)" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Node control stream disconnected");
    expect(onPairingStarted).toHaveBeenCalledWith(["miner-2"]);
    expect(onPairingCompleted).not.toHaveBeenCalled();
  });

  it("defers a late-settlement refresh while a later-page miner is selected", async () => {
    listDiscovered.mockImplementation(async (_nodeId, cursor = 0n) =>
      cursor === 0n ? { devices: [miner], nextCursor: 42n } : { devices: [otherMiner], nextCursor: 0n },
    );
    const props = {
      node,
      canPair: true,
      blockedPairingIdentifiers: [],
      onDismiss: vi.fn(),
      onUpdated: vi.fn(),
      onPairingStarted: vi.fn(),
      onPairingCompleted: vi.fn(),
      onPairingSettledAfterDismiss: vi.fn(),
    };
    const { rerender } = render(<FindNodeMinersModal {...props} settledPairingRevision={0} />);

    await screen.findByRole("checkbox", { name: "Select miner-2" });
    await waitFor(() => expect(screen.getByRole("button", { name: "Load next 100" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Load next 100" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Select miner-3" }));
    rerender(<FindNodeMinersModal {...props} settledPairingRevision={1} />);

    expect(screen.getByRole("checkbox", { name: "Select miner-3" })).toBeChecked();

    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-3" }));
    await waitFor(() => expect(screen.queryByRole("checkbox", { name: "Select miner-3" })).not.toBeInTheDocument());
  });

  it("releases retry blocks after definitive pre-dispatch rejection", async () => {
    pairOnNode.mockRejectedValue(
      createErrorWithCause(
        "fleet node has no active control stream",
        new ConnectError("fleet node has no active control stream", Code.FailedPrecondition),
      ),
    );
    const onPairingCompleted = vi.fn();
    renderFinder({ onPairingCompleted });

    fireEvent.click(await screen.findByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("button", { name: "Add selected (1)" }));

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

    fireEvent.click(await screen.findByRole("checkbox", { name: "Select miner-2" }));
    listDiscovered.mockRejectedValueOnce(new Error("Refresh failed"));
    fireEvent.click(screen.getByRole("button", { name: "Add selected (1)" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Pair result stream disconnected");
    expect(screen.getByText("miner-2: PAIRED")).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Select miner-2" })).not.toBeInTheDocument();
    expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]);
  });
});
