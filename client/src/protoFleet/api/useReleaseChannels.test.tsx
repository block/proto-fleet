import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  ListReleaseChannelsResponseSchema,
  ListRolloutsResponseSchema,
  ReleaseChannelSchema,
  RolloutMethod,
  RolloutSchema,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useReleaseChannels } from "@/protoFleet/api/useReleaseChannels";

const {
  mockListReleaseChannels,
  mockListRollouts,
  mockListMinerStateSnapshots,
  mockCreateReleaseChannel,
  mockUpdateReleaseChannel,
  mockDeleteReleaseChannel,
  mockPreviewReleaseChannelScope,
  mockApplyReleaseChannelFirmware,
  mockCancelRollout,
  mockRetryFailedRolloutDevices,
} = vi.hoisted(() => ({
  mockListReleaseChannels: vi.fn(),
  mockListRollouts: vi.fn(),
  mockListMinerStateSnapshots: vi.fn(),
  mockCreateReleaseChannel: vi.fn(),
  mockUpdateReleaseChannel: vi.fn(),
  mockDeleteReleaseChannel: vi.fn(),
  mockPreviewReleaseChannelScope: vi.fn(),
  mockApplyReleaseChannelFirmware: vi.fn(),
  mockCancelRollout: vi.fn(),
  mockRetryFailedRolloutDevices: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  rolloutClient: {
    listReleaseChannels: mockListReleaseChannels,
    listRollouts: mockListRollouts,
    createReleaseChannel: mockCreateReleaseChannel,
    updateReleaseChannel: mockUpdateReleaseChannel,
    deleteReleaseChannel: mockDeleteReleaseChannel,
    previewReleaseChannelScope: mockPreviewReleaseChannelScope,
    applyReleaseChannelFirmware: mockApplyReleaseChannelFirmware,
    cancelRollout: mockCancelRollout,
    retryFailedRolloutDevices: mockRetryFailedRolloutDevices,
  },
  fleetManagementClient: {
    listMinerStateSnapshots: mockListMinerStateSnapshots,
  },
}));

const canary = create(ReleaseChannelSchema, { id: 1n, name: "Canary", minerCount: 3 });
const rollout = create(RolloutSchema, { id: 9n, channelId: 1n, model: "Rig", status: RolloutStatus.ACTIVE });

describe("useReleaseChannels", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListReleaseChannels.mockResolvedValue(create(ListReleaseChannelsResponseSchema, { channels: [canary] }));
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [rollout] }));
    mockListMinerStateSnapshots.mockResolvedValue({
      miners: [{ deviceIdentifier: "rig-001", name: "Rig A01" }],
    });
  });

  it("loads channels, rollouts and miner names together", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    expect(result.current.isLoading).toBe(true);

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.channels).toEqual([canary]);
    expect(result.current.rollouts).toEqual([rollout]);
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01" });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledWith({ pageSize: 500 });
  });

  it("creates a channel from a draft and refreshes", async () => {
    mockCreateReleaseChannel.mockResolvedValue({ channel: canary });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    const draft = {
      name: "Canary",
      description: "First wave",
      scope: { rackIds: [40n] },
      behavior: { method: RolloutMethod.PILOT_THEN_CONTINUE, pilotSize: 2 },
    };
    let created;
    await act(async () => {
      created = await result.current.createChannel(draft as never);
    });
    expect(created).toEqual(canary);
    expect(mockCreateReleaseChannel).toHaveBeenCalledWith(draft);
    expect(mockListReleaseChannels).toHaveBeenCalledTimes(2);
  });

  it("updates and deletes by channel id", async () => {
    mockUpdateReleaseChannel.mockResolvedValue({ channel: canary });
    mockDeleteReleaseChannel.mockResolvedValue({});
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      await result.current.updateChannel(1n, { name: "Canary 2", description: "", scope: {}, behavior: {} } as never);
      await result.current.deleteChannel(1n);
    });
    expect(mockUpdateReleaseChannel).toHaveBeenCalledWith(expect.objectContaining({ channelId: 1n, name: "Canary 2" }));
    expect(mockDeleteReleaseChannel).toHaveBeenCalledWith({ channelId: 1n });
  });

  it("previews a scope without touching the polled state", async () => {
    mockPreviewReleaseChannelScope.mockResolvedValue({ minerCount: 4, models: [], conflicts: [] });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const listCalls = mockListReleaseChannels.mock.calls.length;

    const preview = await result.current.previewScope({ siteIds: [1n] } as never, 7n);
    expect(preview.minerCount).toBe(4);
    expect(mockPreviewReleaseChannelScope).toHaveBeenCalledWith({ scope: { siteIds: [1n] }, channelId: 7n });
    expect(mockListReleaseChannels).toHaveBeenCalledTimes(listCalls);
  });

  it("applies firmware, cancels and retries by id", async () => {
    mockApplyReleaseChannelFirmware.mockResolvedValue({ startedRollouts: [rollout] });
    mockCancelRollout.mockResolvedValue({ rollout });
    mockRetryFailedRolloutDevices.mockResolvedValue({ rollout });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      const started = await result.current.applyFirmware(1n, [{ model: "Rig", firmwareFileId: "fw-2" }]);
      expect(started).toEqual([rollout]);
      await result.current.cancelRollout(9n);
      const retried = await result.current.retryFailedDevices(9n);
      expect(retried).toEqual(rollout);
    });
    expect(mockApplyReleaseChannelFirmware).toHaveBeenCalledWith({
      channelId: 1n,
      assignments: [{ model: "Rig", firmwareFileId: "fw-2" }],
    });
    expect(mockCancelRollout).toHaveBeenCalledWith({ rolloutId: 9n });
    expect(mockRetryFailedRolloutDevices).toHaveBeenCalledWith({ rolloutId: 9n });
  });
});
