import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";

const listRolloutLanes = vi.hoisted(() => vi.fn());
const listRollouts = vi.hoisted(() => vi.fn());
const listFirmwareFiles = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({ listFirmwareFiles }),
}));

vi.mock("@/protoFleet/api/useRolloutApi", () => ({
  useRolloutApi: () => ({
    lanes: [
      {
        id: "lane-1",
        label: "Stable production",
        description: "",
        currentChannelId: 41n,
        revision: 1n,
        channels: [],
        memberCount: 2,
        memberIdentifiers: [],
        currentReleaseTargets: [],
      },
    ],
    rollouts: [],
    isLoading: false,
    isMutating: false,
    loadError: null,
    mutationError: null,
    permissions: {
      canReadChannels: true,
      canManageChannels: false,
      canRead: true,
      canManage: false,
      canControl: false,
    },
    listRolloutLanes,
    getRolloutLane: vi.fn(),
    createRolloutLane: vi.fn(),
    startRolloutLane: vi.fn(),
    listRollouts,
    admitRollout: vi.fn(),
    continueRollout: vi.fn(),
    pauseRollout: vi.fn(),
    resumeRollout: vi.fn(),
    abortRollout: vi.fn(),
    revertRollout: vi.fn(),
  }),
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/RolloutLanesTable", () => ({
  default: ({ rows }: { rows: Array<{ lane: { label: string } }> }) => <div>{rows[0]?.lane.label}</div>,
}));

const { default: RolloutLanesTab } = await import("./RolloutLanesTab");

describe("RolloutLanesTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listRolloutLanes.mockResolvedValue([]);
    listRollouts.mockResolvedValue([]);
    listFirmwareFiles.mockResolvedValue([]);
  });

  it("loads durable lane state and refreshes rollout records after rollout events", async () => {
    const { unmount } = render(<RolloutLanesTab />);

    await waitFor(() => {
      expect(listRolloutLanes).toHaveBeenCalledTimes(1);
      expect(listRollouts).toHaveBeenCalledTimes(1);
    });
    expect(await screen.findByText("Stable production")).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT));
    });
    await waitFor(() => expect(listRollouts).toHaveBeenCalledTimes(2));

    unmount();
    act(() => {
      window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT));
    });
    expect(listRollouts).toHaveBeenCalledTimes(2);
  });
});
