import { useCallback, useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { fleetManagementClient, rolloutClient } from "@/protoFleet/api/clients";
import {
  type FirmwareAssignment,
  type PreviewReleaseChannelScopeResponse,
  type ReleaseChannel,
  type ReleaseChannelMiner,
  type ReleaseChannelModelGroup,
  type ReleaseChannelScope,
  type Rollout,
  type RolloutBehavior,
  type RolloutDevice,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { rolloutBehaviorForRequest } from "@/protoFleet/api/rolloutBehavior";
import {
  useAuthErrors,
  useFleetStore,
  useIsAuthenticated,
  useSessionGeneration,
  useUsername,
} from "@/protoFleet/store";

// A channel as the UI works with it: the server's channel (scope included)
// together with its manufacturer/model groups, which the API pages
// separately (ListReleaseChannelModelGroups) and the hook drains.
export type ChannelView = ReleaseChannel & { modelGroups: ReleaseChannelModelGroup[] };

// An assignment as the UI stages it: the observed pair the file is for and
// the file (empty to clear).
export type AssignmentDraft = Pick<FirmwareAssignment, "manufacturer" | "model" | "firmwareFileId">;

const POLL_INTERVAL_MS = 5000;
// Largest pages the server allows; lists are read in as few round
// trips as possible.
const DETAIL_PAGE_SIZE = 1000;
const MODEL_GROUP_PAGE_SIZE = 100;

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

async function loadRolloutChanges(pollCursor: string): Promise<{ rollouts: Rollout[]; pollCursor: string }> {
  let nextPollCursor = "";
  const rollouts = await drainPages(async (cursor) => {
    // Keep the preceding cycle's watermark fixed until every page succeeds.
    const response = await rolloutClient.listRollouts({ pageSize: DETAIL_PAGE_SIZE, cursor, pollCursor });
    nextPollCursor = response.pollCursor;
    return { items: response.rollouts, cursor: response.cursor };
  });
  return { rollouts, pollCursor: nextPollCursor };
}

function mergeRollouts(previous: Rollout[], incoming: Rollout[]): Rollout[] {
  const byId = new Map(previous.map((rollout) => [rollout.id, rollout]));
  for (const rollout of incoming) {
    const current = byId.get(rollout.id);
    // Replays must not regress revisions. Equal revisions can still carry
    // fresh live telemetry, which is not part of the revisioned header.
    if (!current || rollout.revision >= current.revision) byId.set(rollout.id, rollout);
  }
  return [...byId.values()].sort((a, b) => {
    const seconds = (b.createdAt?.seconds ?? 0n) - (a.createdAt?.seconds ?? 0n);
    if (seconds !== 0n) return seconds > 0n ? 1 : -1;
    const nanos = (b.createdAt?.nanos ?? 0) - (a.createdAt?.nanos ?? 0);
    if (nanos !== 0) return nanos;
    return a.id === b.id ? 0 : a.id < b.id ? 1 : -1;
  });
}

function rolloutControlRequest(rolloutId: bigint, expectedRevision: bigint) {
  // Zero disables the server's stale-action guard. Controls must use the
  // revision the operator saw, never a newer revision from the polling cache.
  if (!(expectedRevision > 0n)) throw new Error("Refresh the rollout before taking this action.");
  return { rolloutId, expectedRevision };
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
  channels: ChannelView[];
  rollouts: Rollout[];
  // deviceIdentifier -> display name, from fleet snapshots.
  minerNames: Record<string, string>;
  isLoading: boolean;
  refresh: () => Promise<void>;
  createChannel: (draft: ReleaseChannelDraft) => Promise<ChannelView | undefined>;
  updateChannel: (channelId: bigint, draft: ReleaseChannelDraft) => Promise<ChannelView | undefined>;
  deleteChannel: (channelId: bigint) => Promise<void>;
  // Read-only: does not touch the polled state.
  previewScope: (scope: ReleaseChannelScope, channelId?: bigint) => Promise<PreviewReleaseChannelScopeResponse>;
  // Read-only detail lists. The server pages both; these walk every page so
  // a modal can show the whole set. Filters match observed identities
  // verbatim.
  listChannelMiners: (channelId: bigint, manufacturer?: string, model?: string) => Promise<ReleaseChannelMiner[]>;
  listRolloutDevices: (rolloutId: bigint) => Promise<RolloutDevice[]>;
  applyFirmware: (channelId: bigint, assignments: AssignmentDraft[]) => Promise<Rollout[]>;
  rollbackFirmware: (rolloutId: bigint, expectedRevision: bigint) => Promise<Rollout[]>;
  continueRollout: (rolloutId: bigint, expectedRevision: bigint) => Promise<void>;
  pauseRollout: (rolloutId: bigint, expectedRevision: bigint) => Promise<void>;
  resumeRollout: (rolloutId: bigint, expectedRevision: bigint) => Promise<void>;
  cancelRollout: (rolloutId: bigint, expectedRevision: bigint) => Promise<void>;
  retryFailedDevices: (rolloutId: bigint, expectedRevision: bigint) => Promise<Rollout | undefined>;
}

// Fetches release channels and rollouts, polling while mounted so firmware
// versions and update progress stay live.
// Loads a channel with its scope and every model group page.
async function loadChannel(channelId: bigint): Promise<ChannelView | undefined> {
  const [detail, modelGroups] = await Promise.all([
    rolloutClient.getReleaseChannel({ channelId }),
    drainPages((cursor) =>
      rolloutClient
        .listReleaseChannelModelGroups({ channelId, pageSize: MODEL_GROUP_PAGE_SIZE, cursor })
        .then((resp) => ({ items: resp.modelGroups, cursor: resp.cursor })),
    ),
  ]);
  return detail.channel ? { ...detail.channel, modelGroups } : undefined;
}

export function useReleaseChannels(): ReleaseChannelsApi {
  const { handleAuthErrors } = useAuthErrors();
  const sessionGeneration = useSessionGeneration();
  const username = useUsername();
  const isAuthenticated = useIsAuthenticated();
  const authSessionIdentity = `${username}:${sessionGeneration}`;
  const [snapshot, setSnapshot] = useState({
    authSessionIdentity,
    channels: [] as ChannelView[],
    rollouts: [] as Rollout[],
    minerNames: {} as Record<string, string>,
    isLoading: true,
  });
  const inFlightRef = useRef<Promise<void> | null>(null);
  const sessionRef = useRef<object | null>(null);
  const rolloutSnapshotRef = useRef({ rollouts: [] as Rollout[], pollCursor: "" });

  const isCurrentSession = useCallback(() => {
    const auth = useFleetStore.getState().auth;
    return (
      isAuthenticated &&
      auth.isAuthenticated &&
      auth.sessionGeneration === sessionGeneration &&
      auth.username === username
    );
  }, [isAuthenticated, sessionGeneration, username]);

  const fetchState = useCallback(
    async (session: object) => {
      const isCurrentRequest = () => sessionRef.current === session && isCurrentSession();
      try {
        const previous = rolloutSnapshotRef.current;
        const [changes, activeRollouts, miners] = await Promise.all([
          loadRolloutChanges(previous.pollCursor),
          // Telemetry/evidence can change without a header revision. Refresh
          // active rows separately; this work does not grow with history.
          previous.pollCursor
            ? drainPages((cursor) =>
                rolloutClient
                  .listRollouts({ pageSize: DETAIL_PAGE_SIZE, cursor, status: RolloutStatus.ACTIVE })
                  .then((resp) => ({ items: resp.rollouts, cursor: resp.cursor })),
              )
            : Promise.resolve([] as Rollout[]),
          drainPages((cursor) =>
            fleetManagementClient
              .listMinerStateSnapshots({ pageSize: DETAIL_PAGE_SIZE, cursor })
              .then((resp) => ({ items: resp.miners, cursor: resp.cursor })),
          ).catch((error: unknown) => {
            // Firmware managers need not have miner:read. Names are optional;
            // do not retain old names after their permission is revoked.
            if (error instanceof ConnectError && error.code === Code.PermissionDenied) return [];
            throw error;
          }),
        ]);
        if (!isCurrentRequest()) return;
        // Read channels after rollouts, so a newly created channel cannot be
        // mistaken for a deletion when its rollout arrives in this cycle.
        const channelSummaries = await drainPages((cursor) =>
          rolloutClient
            .listReleaseChannels({ pageSize: DETAIL_PAGE_SIZE, cursor })
            .then((resp) => ({ items: resp.channels, cursor: resp.cursor })),
        );
        if (!isCurrentRequest()) return;
        // The list carries summaries; scope and groups come per channel.
        const views = await Promise.all(channelSummaries.map((summary) => loadChannel(summary.id)));
        if (!isCurrentRequest()) return;
        const nextChannels = views.filter((view): view is ChannelView => view !== undefined);
        const channelsById = new Map(nextChannels.map((channel) => [channel.id, channel]));
        const allRollouts = mergeRollouts(previous.pollCursor ? previous.rollouts : [], [
          ...changes.rollouts,
          ...activeRollouts,
        ])
          // Channel deletion cascades to rollouts without a delta tombstone.
          .filter((rollout) => channelsById.has(rollout.channelId))
          .map((rollout) => ({ ...rollout, channelName: channelsById.get(rollout.channelId)!.name }));
        // Commit the watermark with the complete UI snapshot. A failure in
        // any list or channel detail must replay the same delta on retry.
        rolloutSnapshotRef.current = { rollouts: allRollouts, pollCursor: changes.pollCursor };
        setSnapshot({
          authSessionIdentity,
          channels: nextChannels,
          rollouts: allRollouts,
          minerNames: Object.fromEntries(miners.map((miner) => [miner.deviceIdentifier, miner.name])),
          isLoading: false,
        });
      } catch (error) {
        // A delayed 401 from the previous login must not log out its replacement.
        if (!isCurrentRequest()) return;
        setSnapshot((previous) =>
          previous.authSessionIdentity === authSessionIdentity
            ? { ...previous, isLoading: false }
            : { authSessionIdentity, channels: [], rollouts: [], minerNames: {}, isLoading: false },
        );
        handleAuthErrors({ error });
        throw error;
      }
    },
    [authSessionIdentity, handleAuthErrors, isCurrentSession],
  );

  const refresh = useCallback(async () => {
    const session = sessionRef.current;
    if (!session || !isCurrentSession()) return;
    // A mutation must read state fetched after it completed. Wait for any
    // older request (including a failed poll), then start a fresh one.
    while (inFlightRef.current) {
      await inFlightRef.current.catch(() => undefined);
      if (sessionRef.current !== session || !isCurrentSession()) return;
    }
    const request = fetchState(session);
    inFlightRef.current = request;
    try {
      await request;
    } finally {
      if (inFlightRef.current === request) inFlightRef.current = null;
    }
  }, [fetchState, isCurrentSession]);

  useEffect(() => {
    // A new login needs its own baseline and must not wait for old requests.
    sessionRef.current = {};
    inFlightRef.current = null;
    rolloutSnapshotRef.current = { rollouts: [], pollCursor: "" };
    const poll = () => {
      // Timer ticks never queue work behind an existing refresh.
      if (inFlightRef.current) return;
      refresh().catch((error) => console.error("Failed to refresh release channels", error));
    };
    poll();
    const timer = setInterval(poll, POLL_INTERVAL_MS);
    return () => {
      clearInterval(timer);
      sessionRef.current = null;
    };
  }, [refresh]);

  const createChannel = useCallback(
    async (draft: ReleaseChannelDraft) => {
      const resp = await rolloutClient.createReleaseChannel({
        ...draft,
        behavior: rolloutBehaviorForRequest(draft.behavior),
      });
      const view = resp.channel ? await loadChannel(resp.channel.id) : undefined;
      await refresh();
      return view;
    },
    [refresh],
  );

  const updateChannel = useCallback(
    async (channelId: bigint, draft: ReleaseChannelDraft) => {
      await rolloutClient.updateReleaseChannel({
        channelId,
        ...draft,
        behavior: rolloutBehaviorForRequest(draft.behavior),
      });
      const view = await loadChannel(channelId);
      await refresh();
      return view;
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
    (channelId: bigint, manufacturer?: string, model?: string) =>
      drainPages((cursor) =>
        rolloutClient
          .listReleaseChannelMiners({
            channelId,
            manufacturer: manufacturer ?? "",
            model: model ?? "",
            pageSize: DETAIL_PAGE_SIZE,
            cursor,
          })
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
    async (channelId: bigint, assignments: AssignmentDraft[]) => {
      const resp = await rolloutClient.applyReleaseChannelFirmware({
        channelId,
        assignments: assignments.map((a) => ({
          manufacturer: a.manufacturer,
          model: a.model,
          firmwareFileId: a.firmwareFileId,
        })),
      });
      await refresh();
      return resp.startedRollouts;
    },
    [refresh],
  );

  const rollbackFirmware = useCallback(
    async (rolloutId: bigint, expectedRevision: bigint) => {
      const resp = await rolloutClient.rollbackReleaseChannelFirmware(
        rolloutControlRequest(rolloutId, expectedRevision),
      );
      await refresh();
      return resp.startedRollouts;
    },
    [refresh],
  );

  const continueRollout = useCallback(
    async (rolloutId: bigint, expectedRevision: bigint) => {
      await rolloutClient.continueRollout(rolloutControlRequest(rolloutId, expectedRevision));
      await refresh();
    },
    [refresh],
  );

  const pauseRollout = useCallback(
    async (rolloutId: bigint, expectedRevision: bigint) => {
      await rolloutClient.pauseRollout(rolloutControlRequest(rolloutId, expectedRevision));
      await refresh();
    },
    [refresh],
  );

  const resumeRollout = useCallback(
    async (rolloutId: bigint, expectedRevision: bigint) => {
      await rolloutClient.resumeRollout(rolloutControlRequest(rolloutId, expectedRevision));
      await refresh();
    },
    [refresh],
  );

  const cancelRollout = useCallback(
    async (rolloutId: bigint, expectedRevision: bigint) => {
      await rolloutClient.cancelRollout(rolloutControlRequest(rolloutId, expectedRevision));
      await refresh();
    },
    [refresh],
  );

  const retryFailedDevices = useCallback(
    async (rolloutId: bigint, expectedRevision: bigint) => {
      const resp = await rolloutClient.retryFailedRolloutDevices(rolloutControlRequest(rolloutId, expectedRevision));
      await refresh();
      return resp.rollout;
    },
    [refresh],
  );

  const hasCurrentSnapshot = isAuthenticated && snapshot.authSessionIdentity === authSessionIdentity;
  return {
    channels: hasCurrentSnapshot ? snapshot.channels : [],
    rollouts: hasCurrentSnapshot ? snapshot.rollouts : [],
    minerNames: hasCurrentSnapshot ? snapshot.minerNames : {},
    isLoading: isAuthenticated && (!hasCurrentSnapshot || snapshot.isLoading),
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
