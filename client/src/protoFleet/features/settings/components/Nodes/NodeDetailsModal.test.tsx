import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import NodeDetailsModal from "./NodeDetailsModal";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import {
  DevicePairingResultSchema,
  FleetNodeDeviceSummarySchema,
  FleetNodeDiscoveredDeviceSchema,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";

const listPairs = vi.hoisted(() => vi.fn());
const listDiscovered = vi.hoisted(() => vi.fn());
const discoverOnNode = vi.hoisted(() => vi.fn());
const pairOnNode = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useFleetNodes", () => ({
  useFleetNodes: () => ({
    listFleetNodeDevices: listPairs,
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

beforeEach(() => {
  listPairs
    .mockReset()
    .mockResolvedValue([
      create(FleetNodeDeviceSummarySchema, { fleetNodeId: 7n, deviceId: 1n, deviceIdentifier: "miner-1" }),
    ]);
  listDiscovered.mockReset().mockResolvedValue({ devices: [miner], nextCursor: 0n });
  discoverOnNode.mockReset().mockResolvedValue(undefined);
  pairOnNode.mockReset().mockResolvedValue(undefined);
});

describe("NodeDetailsModal", () => {
  it("shows paired and discovered miners to read-only operators", async () => {
    render(<NodeDetailsModal node={node} canManage={false} canPair={false} onDismiss={vi.fn()} onUpdated={vi.fn()} />);

    expect(await screen.findByText("miner-1")).toBeInTheDocument();
    expect(screen.getByText(/miner-2/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Run discovery" })).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Select miner-2" })).not.toBeInTheDocument();
    expect(listPairs).toHaveBeenCalledWith("7");
    expect(listDiscovered).toHaveBeenCalledWith("7");
  });

  it("scans an explicit target on this Node and shows its progress", async () => {
    discoverOnNode.mockImplementation(async (_id, _request, onResponse) => {
      onResponse({ devices: [{ deviceIdentifier: "miner-3" }], warning: "" });
    });
    render(<NodeDetailsModal node={node} canManage canPair onDismiss={vi.fn()} onUpdated={vi.fn()} />);

    await screen.findByText("miner-1");
    fireEvent.change(screen.getByLabelText("Scan target"), { target: { value: "192.168.1.0/24" } });
    fireEvent.click(screen.getByRole("button", { name: "Run discovery" }));

    await waitFor(() => expect(discoverOnNode).toHaveBeenCalledTimes(1));
    expect(discoverOnNode.mock.calls[0][0]).toBe("7");
    expect(discoverOnNode.mock.calls[0][1].mode).toMatchObject({
      case: "networkScan",
      value: { target: "192.168.1.0/24" },
    });
    expect(await screen.findByText("Scan complete: 1 found")).toBeInTheDocument();
  });

  it("pairs only selected miners and reports the streamed result", async () => {
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, { deviceIdentifier: "miner-2", pairingStatus: PairingStatus.PAIRED }),
      ]);
    });
    const onUpdated = vi.fn();
    render(<NodeDetailsModal node={node} canManage canPair onDismiss={vi.fn()} onUpdated={onUpdated} />);

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));

    await waitFor(() => expect(pairOnNode).toHaveBeenCalledTimes(1));
    expect(pairOnNode.mock.calls[0][0]).toBe("7");
    expect(pairOnNode.mock.calls[0][1]).toEqual(["miner-2"]);
    expect(pairOnNode.mock.calls[0][2]).toMatchObject({ username: "admin", password: "secret" });
    expect(await screen.findByText("miner-2: PAIRED")).toBeInTheDocument();
    await waitFor(() => expect(onUpdated).toHaveBeenCalled());
  });
});
