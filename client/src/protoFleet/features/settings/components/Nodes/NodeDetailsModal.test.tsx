import type { ComponentProps } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import NodeDetailsModal from "./NodeDetailsModal";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import {
  DevicePairingResultSchema,
  FleetNodeDeviceSummarySchema,
  FleetNodeDiscoveredDeviceSchema,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeDiscoveredDevice } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { createErrorWithCause } from "@/protoFleet/api/requestErrors";
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
const otherMiner = create(FleetNodeDiscoveredDeviceSchema, {
  fleetNodeId: 7n,
  deviceIdentifier: "miner-3",
  ipAddress: "192.168.1.3",
});

const renderDetails = (props: Partial<ComponentProps<typeof NodeDetailsModal>> = {}) =>
  render(
    <NodeDetailsModal
      node={node}
      canManage
      canPair
      blockedPairingIdentifiers={[]}
      onDismiss={vi.fn()}
      onUpdated={vi.fn()}
      onPairingStarted={vi.fn()}
      onPairingCompleted={vi.fn()}
      onPairingSettledAfterDismiss={vi.fn()}
      {...props}
    />,
  );

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
    renderDetails({ canManage: false, canPair: false });

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
    renderDetails();

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
    renderDetails({ onUpdated });

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

  it("does not show empty miner lists when the initial load fails", async () => {
    listPairs.mockRejectedValue(new Error("Paired miners unavailable"));
    renderDetails();

    expect(await screen.findByRole("alert")).toHaveTextContent("Paired miners unavailable");
    await waitFor(() => expect(screen.queryByText("Loading miners…")).not.toBeInTheDocument());
    expect(screen.queryByText("No paired miners.")).not.toBeInTheDocument();
    expect(screen.queryByText("No unpaired miners discovered.")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Run discovery" })).not.toBeInTheDocument();
  });

  it("drops a page response that arrives after discovery refreshes the list", async () => {
    let resolvePage!: (value: { devices: FleetNodeDiscoveredDevice[]; nextCursor: bigint }) => void;
    const pendingPage = new Promise<{ devices: FleetNodeDiscoveredDevice[]; nextCursor: bigint }>((resolve) => {
      resolvePage = resolve;
    });
    let firstPageCalls = 0;
    listDiscovered.mockImplementation((_id, cursor = 0n) => {
      if (cursor !== 0n) return pendingPage;
      firstPageCalls++;
      return Promise.resolve({ devices: firstPageCalls === 1 ? [miner] : [otherMiner], nextCursor: 1n });
    });
    renderDetails();

    await screen.findByText(/miner-2 ·/);
    fireEvent.click(screen.getByRole("button", { name: "Load next 100" }));
    await waitFor(() => expect(listDiscovered).toHaveBeenCalledWith("7", 1n));
    fireEvent.change(screen.getByLabelText("Scan target"), { target: { value: "192.168.1.0/24" } });
    fireEvent.click(screen.getByRole("button", { name: "Run discovery" }));
    expect(await screen.findByText(/miner-3 ·/)).toBeInTheDocument();

    resolvePage({ devices: [miner], nextCursor: 0n });
    await waitFor(() => expect(screen.queryByText(/miner-2 ·/)).not.toBeInTheDocument());
    expect(screen.getAllByText(/miner-3 ·/)).toHaveLength(1);
  });

  it("clears selections that are no longer visible after a rescan", async () => {
    let firstPageCalls = 0;
    listDiscovered.mockImplementation((_id, cursor = 0n) => {
      if (cursor !== 0n) return Promise.resolve({ devices: [otherMiner], nextCursor: 0n });
      firstPageCalls++;
      return Promise.resolve({ devices: [miner], nextCursor: firstPageCalls === 1 ? 1n : 0n });
    });
    renderDetails();

    await screen.findByText(/miner-2 ·/);
    fireEvent.click(screen.getByRole("button", { name: "Load next 100" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Select miner-3" }));
    expect(screen.getByRole("button", { name: "Pair selected (1)" })).toBeEnabled();

    fireEvent.change(screen.getByLabelText("Scan target"), { target: { value: "192.168.1.0/24" } });
    fireEvent.click(screen.getByRole("button", { name: "Run discovery" }));
    await waitFor(() => expect(screen.queryByRole("checkbox", { name: "Select miner-3" })).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Pair selected (0)" })).toBeDisabled();
  });

  it("keeps partial results and the original error when pairing and recovery fail", async () => {
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, { deviceIdentifier: "miner-2", pairingStatus: PairingStatus.PAIRED }),
      ]);
      throw new Error("Pair result stream disconnected");
    });
    const onUpdated = vi.fn();
    renderDetails({ onUpdated });

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    listPairs.mockRejectedValueOnce(new Error("Refresh failed"));
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Pair result stream disconnected");
    expect(screen.getByText("miner-2: PAIRED")).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Select miner-2" })).not.toBeInTheDocument();
    expect(pairOnNode).toHaveBeenCalledTimes(1);
    expect(onUpdated).not.toHaveBeenCalled();
  });

  it("refreshes both lists after an interrupted pairing stream", async () => {
    pairOnNode.mockRejectedValue(new Error("Pair result stream disconnected"));
    const onUpdated = vi.fn();
    const onPairingStarted = vi.fn();
    const onPairingCompleted = vi.fn();
    renderDetails({ onUpdated, onPairingStarted, onPairingCompleted });

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Pair result stream disconnected");
    await waitFor(() => expect(onUpdated).toHaveBeenCalledTimes(1));
    expect(listPairs).toHaveBeenCalledTimes(2);
    expect(listDiscovered).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("button", { name: "Pair selected (0)" })).toBeDisabled();
    expect(pairOnNode).toHaveBeenCalledTimes(1);
    expect(onPairingStarted).toHaveBeenCalledWith(["miner-2"]);
    expect(onPairingCompleted).not.toHaveBeenCalled();
  });

  it("releases reported miners while leaving unreported miners blocked after a stream failure", async () => {
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
    renderDetails({ onPairingStarted, onPairingCompleted });

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-3" }));
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (2)" }));

    await waitFor(() => expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]));
    expect(onPairingStarted).toHaveBeenCalledWith(["miner-2", "miner-3"]);
    expect(onPairingCompleted).toHaveBeenCalledTimes(1);
    expect(await screen.findByRole("alert")).toHaveTextContent("Pair result stream disconnected");
  });

  it("removes a reported paired miner from a stale list when recovery fails", async () => {
    pairOnNode.mockImplementation(async (_id, _ids, _credentials, onResults) => {
      onResults([
        create(DevicePairingResultSchema, { deviceIdentifier: "miner-2", pairingStatus: PairingStatus.PAIRED }),
      ]);
      throw new Error("Pair result stream disconnected");
    });
    const onPairingCompleted = vi.fn();
    renderDetails({ onPairingCompleted });

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    listPairs.mockRejectedValueOnce(new Error("Refresh failed"));
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));

    await waitFor(() => expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]));
    expect(await screen.findByRole("alert")).toHaveTextContent("Pair result stream disconnected");
    expect(screen.queryByRole("checkbox", { name: "Select miner-2" })).not.toBeInTheDocument();
  });

  it("prevents selecting a miner whose earlier pairing result is unknown", async () => {
    renderDetails({ blockedPairingIdentifiers: ["miner-2"] });

    await screen.findByText("miner-1");
    expect(screen.getByRole("checkbox", { name: "Select miner-2" })).toBeDisabled();
    expect(screen.getByRole("status")).toHaveTextContent("1 miner has an unknown pairing result");
    expect(screen.getByRole("button", { name: "Pair selected (0)" })).toBeDisabled();
  });

  it("requires a password with a supplied username", async () => {
    renderDetails();

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "admin" } });
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));

    expect(screen.getByRole("alert")).toHaveTextContent("Enter both a username and password");
    expect(pairOnNode).not.toHaveBeenCalled();
  });

  it("clears the retry block after a definitive pre-dispatch rejection", async () => {
    pairOnNode.mockRejectedValue(
      createErrorWithCause(
        "fleet node has no active control stream",
        new ConnectError("fleet node has no active control stream", Code.FailedPrecondition),
      ),
    );
    const onPairingCompleted = vi.fn();
    renderDetails({ onPairingCompleted });

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));

    await waitFor(() => expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]));
    expect(pairOnNode).toHaveBeenCalledTimes(1);
  });

  it("keeps the retry block after a stream failure with the same error code", async () => {
    pairOnNode.mockRejectedValue(
      createErrorWithCause(
        "fleet node control stream closed before command completed",
        new ConnectError("fleet node control stream closed before command completed", Code.FailedPrecondition),
      ),
    );
    const onPairingCompleted = vi.fn();
    renderDetails({ onPairingCompleted });

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("control stream closed");
    expect(onPairingCompleted).not.toHaveBeenCalled();
  });

  it("notifies the page when pairing finishes after the detail view is dismissed", async () => {
    let finishPairing!: () => void;
    pairOnNode.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finishPairing = resolve;
        }),
    );
    const onPairingSettledAfterDismiss = vi.fn();
    const onPairingCompleted = vi.fn();
    const { unmount } = renderDetails({ onPairingSettledAfterDismiss, onPairingCompleted });

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));
    await waitFor(() => expect(pairOnNode).toHaveBeenCalledTimes(1));
    unmount();
    finishPairing();

    await waitFor(() => expect(onPairingSettledAfterDismiss).toHaveBeenCalledTimes(1));
    expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]);
  });

  it("releases completed pairing even when its list refresh fails", async () => {
    const onPairingCompleted = vi.fn();
    renderDetails({ onPairingCompleted });

    await screen.findByText("miner-1");
    fireEvent.click(screen.getByRole("checkbox", { name: "Select miner-2" }));
    listPairs.mockRejectedValue(new Error("Paired miners unavailable"));
    fireEvent.click(screen.getByRole("button", { name: "Pair selected (1)" }));

    await waitFor(() => expect(onPairingCompleted).toHaveBeenCalledWith(["miner-2"]));
    expect(await screen.findByRole("alert")).toHaveTextContent("Paired miners unavailable");
    expect(screen.queryByRole("checkbox", { name: "Select miner-2" })).not.toBeInTheDocument();
    expect(onPairingCompleted).toHaveBeenCalledTimes(1);
  });
});
