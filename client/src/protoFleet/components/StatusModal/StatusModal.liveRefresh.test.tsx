import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { ProtoFleetStatusModal } from ".";
import {
  MinerStateSnapshotSchema,
  RefreshMinersResponseSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// Capture the tick the modal hands to the live-refresh hook so we can drive it
// deterministically without wiring up real timers/visibility here — that
// lifecycle is covered by useModalLiveRefresh.test.ts.
let capturedOnTick: (() => Promise<void>) | null = null;
vi.mock("./hooks/useModalLiveRefresh", () => ({
  useModalLiveRefresh: (opts: { onTick: () => Promise<void> }) => {
    capturedOnTick = opts.onTick;
    return { isPaused: false, resume: vi.fn() };
  },
}));

const { mockRefreshMiners, mockRefetchErrors } = vi.hoisted(() => ({
  mockRefreshMiners: vi.fn(),
  mockRefetchErrors: vi.fn(),
}));

vi.mock("@/protoFleet/api/useRefreshMiners", () => ({
  default: () => ({ refreshMiners: mockRefreshMiners, refreshing: new Set<string>() }),
}));

vi.mock("@/protoFleet/api/useDeviceErrors", () => ({
  useDeviceErrors: () => ({
    errorsByDevice: {},
    isLoading: false,
    hasLoaded: true,
    error: null,
    refetch: mockRefetchErrors,
  }),
}));

vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: () => ({ startMining: vi.fn() }),
}));

// The shared modal chrome is irrelevant to the refresh wiring.
vi.mock("@/shared/components/StatusModal", () => ({
  StatusModal: () => <div data-testid="shared-status-modal" />,
}));

const miner = create(MinerStateSnapshotSchema, { deviceIdentifier: "miner-1", name: "Miner 1" });

describe("ProtoFleetStatusModal live refresh wiring", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    capturedOnTick = null;
    mockRefetchErrors.mockResolvedValue(undefined);
  });

  it("merges refreshed snapshots and refetches errors on each tick", async () => {
    const snapshot = create(MinerStateSnapshotSchema, { deviceIdentifier: "miner-1", name: "Miner 1 (fresh)" });
    mockRefreshMiners.mockResolvedValue(create(RefreshMinersResponseSchema, { snapshots: [snapshot], errors: {} }));
    const onMergeMiners = vi.fn();

    render(
      <ProtoFleetStatusModal open onClose={vi.fn()} deviceId="miner-1" miner={miner} onMergeMiners={onMergeMiners} />,
    );

    expect(capturedOnTick).toBeTypeOf("function");
    await capturedOnTick!();

    expect(mockRefreshMiners).toHaveBeenCalledWith(["miner-1"]);
    expect(onMergeMiners).toHaveBeenCalledWith([snapshot]);
    expect(mockRefetchErrors).toHaveBeenCalledTimes(1);
  });

  it("keeps the last-good snapshot when a refresh fails, but still refetches errors", async () => {
    mockRefreshMiners.mockRejectedValue(new Error("network"));
    const onMergeMiners = vi.fn();

    render(
      <ProtoFleetStatusModal open onClose={vi.fn()} deviceId="miner-1" miner={miner} onMergeMiners={onMergeMiners} />,
    );

    await capturedOnTick!();

    expect(onMergeMiners).not.toHaveBeenCalled();
    expect(mockRefetchErrors).toHaveBeenCalledTimes(1);
  });

  it("does not merge when the refresh returns no snapshots", async () => {
    mockRefreshMiners.mockResolvedValue(create(RefreshMinersResponseSchema, { snapshots: [], errors: {} }));
    const onMergeMiners = vi.fn();

    render(
      <ProtoFleetStatusModal open onClose={vi.fn()} deviceId="miner-1" miner={miner} onMergeMiners={onMergeMiners} />,
    );

    await capturedOnTick!();

    expect(onMergeMiners).not.toHaveBeenCalled();
    expect(mockRefetchErrors).toHaveBeenCalledTimes(1);
  });
});
