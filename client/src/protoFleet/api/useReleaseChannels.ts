import { useCallback, useEffect, useRef, useState } from "react";

import { fleetManagementClient, rolloutClient } from "@/protoFleet/api/clients";
import type {
  FirmwareAssignment,
  PreviewReleaseChannelScopeResponse,
  ReleaseChannel,
  ReleaseChannelMiner,
  ReleaseChannelScope,
  Rollout,
  RolloutBehavior,
  RolloutDevice,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const POLL_INTERVAL_MS = 5000;
// Largest page the server allows; detail lists are read in as few round
// trips as possible.
const DETAIL_PAGE_SIZE = 1000;

// Follows a cursor-paged list to its end.
async function drainPages<T>(fetchPage: (cursor: string) => Promise<{ items: T[]; cursor: string }>): Promise<T[]> {
  const all: T[] = [];
  let cursor = "";
  do {
    const page = await fetchPage(cursor);
    all.push(...page.items);
    cursor = page.cursor;
  } while (cursor !== "");
  return all;
}

// What an operator sets on a channel; the server resolves the scope and
// validates the behavior.
export interface ReleaseChannelDraft {
  name: string;
  description: string;
  scope: ReleaseChannelScope;
  behavior: RolloutBehavior;
}

export interface ReleaseChannelsApi {
  channels: ReleaseChannel[];
  rollouts: Rollout[];
  // deviceIdentifier -> display name, from fleet snapshots.
  minerNames: Record<string, string>;
  isLoading: boolean;
  refresh: () => Promise<void>;
  createChannel: (draft: ReleaseChannelDraft) => Promise<ReleaseChannel | undefined>;
  updateChannel: (channelId: bigint, draft: ReleaseChannelDraft) => Promise<ReleaseChannel | undefined>;
  deleteChannel: (channelId: bigint) => Promise<void>;
  // Read-only: does not touch the polled state.
  previewScope: (scope: ReleaseChannelScope, channelId?: bigint) => Promise<PreviewReleaseChannelScopeResponse>;
  // Read-only detail lists. The server pages both; these walk every page so
  // a modal can show the whole set.
  listChannelMiners: (channelId: bigint, model?: string) => Promise<ReleaseChannelMiner[]>;
  listRolloutDevices: (rolloutId: bigint) => Promise<RolloutDevice[]>;
  applyFirmware: (
    channelId: bigint,
    assignments: Pick<FirmwareAssignment, "model" | "firmwareFileId">[],
  ) => Promise<Rollout[]>;
  rollbackFirmware: (rolloutId: bigint) => Promise<Rollout[]>;
  continueRollout: (rolloutId: bigint) => Promise<void>;
  pauseRollout: (rolloutId: bigint) => Promise<void>;
  resumeRollout: (rolloutId: bigint) => Promise<void>;
  cancelRollout: (rolloutId: bigint) => Promise<void>;
  retryFailedDevices: (rolloutId: bigint) => Promise<Rollout | undefined>;
}

// Fetches release channels and rollouts, polling while mounted so firmware
// versions and update progress stay live.
export function useReleaseChannels(): ReleaseChannelsApi {
  const [channels, setChannels] = useState<ReleaseChannel[]>([]);
  const [rollouts, setRollouts] = useState<Rollout[]>([]);
  const [minerNames, setMinerNames] = useState<Record<string, string>>({});
  const [isLoading, setIsLoading] = useState(true);
  const inFlightRef = useRef(false);

  const refresh = useCallback(async () => {
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    try {
      const [channelsResp, rolloutsResp, minersResp] = await Promise.all([
        rolloutClient.listReleaseChannels({}),
        rolloutClient.listRollouts({}),
        fleetManagementClient.listMinerStateSnapshots({ pageSize: 500 }),
      ]);
      setChannels(channelsResp.channels);
      setRollouts(rolloutsResp.rollouts);
      setMinerNames(Object.fromEntries(minersResp.miners.map((miner) => [miner.deviceIdentifier, miner.name])));
    } finally {
      inFlightRef.current = false;
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh().catch((error) => console.error("Failed to load release channels", error));
    const timer = setInterval(() => {
      refresh().catch((error) => console.error("Failed to refresh release channels", error));
    }, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  const createChannel = useCallback(
    async (draft: ReleaseChannelDraft) => {
      const resp = await rolloutClient.createReleaseChannel(draft);
      await refresh();
      return resp.channel;
    },
    [refresh],
  );

  const updateChannel = useCallback(
    async (channelId: bigint, draft: ReleaseChannelDraft) => {
      const resp = await rolloutClient.updateReleaseChannel({ channelId, ...draft });
      await refresh();
      return resp.channel;
    },
    [refresh],
  );

  const deleteChannel = useCallback(
    async (channelId: bigint) => {
      await rolloutClient.deleteReleaseChannel({ channelId });
      await refresh();
    },
    [refresh],
  );

  const previewScope = useCallback(
    (scope: ReleaseChannelScope, channelId?: bigint) =>
      rolloutClient.previewReleaseChannelScope({ scope, channelId: channelId ?? 0n }),
    [],
  );

  const listChannelMiners = useCallback(
    (channelId: bigint, model?: string) =>
      drainPages((cursor) =>
        rolloutClient
          .listReleaseChannelMiners({ channelId, model: model ?? "", pageSize: DETAIL_PAGE_SIZE, cursor })
          .then((resp) => ({ items: resp.miners, cursor: resp.cursor })),
      ),
    [],
  );

  const listRolloutDevices = useCallback(
    (rolloutId: bigint) =>
      drainPages((cursor) =>
        rolloutClient
          .listRolloutDevices({ rolloutId, pageSize: DETAIL_PAGE_SIZE, cursor })
          .then((resp) => ({ items: resp.devices, cursor: resp.cursor })),
      ),
    [],
  );

  const applyFirmware = useCallback(
    async (channelId: bigint, assignments: Pick<FirmwareAssignment, "model" | "firmwareFileId">[]) => {
      const resp = await rolloutClient.applyReleaseChannelFirmware({
        channelId,
        assignments: assignments.map((a) => ({ model: a.model, firmwareFileId: a.firmwareFileId })),
      });
      await refresh();
      return resp.startedRollouts;
    },
    [refresh],
  );

  const rollbackFirmware = useCallback(
    async (rolloutId: bigint) => {
      const resp = await rolloutClient.rollbackReleaseChannelFirmware({ rolloutId });
      await refresh();
      return resp.startedRollouts;
    },
    [refresh],
  );

  const continueRollout = useCallback(
    async (rolloutId: bigint) => {
      await rolloutClient.continueRollout({ rolloutId });
      await refresh();
    },
    [refresh],
  );

  const pauseRollout = useCallback(
    async (rolloutId: bigint) => {
      await rolloutClient.pauseRollout({ rolloutId });
      await refresh();
    },
    [refresh],
  );

  const resumeRollout = useCallback(
    async (rolloutId: bigint) => {
      await rolloutClient.resumeRollout({ rolloutId });
      await refresh();
    },
    [refresh],
  );

  const cancelRollout = useCallback(
    async (rolloutId: bigint) => {
      await rolloutClient.cancelRollout({ rolloutId });
      await refresh();
    },
    [refresh],
  );

  const retryFailedDevices = useCallback(
    async (rolloutId: bigint) => {
      const resp = await rolloutClient.retryFailedRolloutDevices({ rolloutId });
      await refresh();
      return resp.rollout;
    },
    [refresh],
  );

  return {
    channels,
    rollouts,
    minerNames,
    isLoading,
    refresh,
    createChannel,
    updateChannel,
    deleteChannel,
    previewScope,
    listChannelMiners,
    listRolloutDevices,
    applyFirmware,
    rollbackFirmware,
    continueRollout,
    pauseRollout,
    resumeRollout,
    cancelRollout,
    retryFailedDevices,
  };
}
