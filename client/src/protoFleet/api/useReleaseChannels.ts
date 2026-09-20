import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
import { acknowledgeRollout, isRollbackAcknowledged } from "@/protoFleet/api/rollbackAcknowledgements";
import { rolloutBehaviorForRequest } from "@/protoFleet/api/rolloutBehavior";
import { mergeRollouts } from "@/protoFleet/api/rolloutSnapshots";
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
export type ChannelModelGroupView = ReleaseChannelModelGroup & { rollbackPending?: boolean };
export type ChannelView = ReleaseChannel & { modelGroups: ChannelModelGroupView[] };

// An assignment as the UI stages it: the observed pair the file is for and
// the file (empty to clear).
export type AssignmentDraft = Pick<FirmwareAssignment, "manufacturer" | "model" | "firmwareFileId">;

const POLL_INTERVAL_MS = 5000;
const MINER_NAMES_REFRESH_INTERVAL_MS = 5 * 60_000;
// Bound each read, including later pages, so a stalled connection cannot
// hold the refresh lock indefinitely. Large scans get a fresh budget per RPC.
const POLL_RPC_TIMEOUT_MS = 30_000;
const DETAIL_RPC_TIMEOUT_MS = 30_000;
// Largest pages the server allows; lists are read in as few round
// trips as possible.
const DETAIL_PAGE_SIZE = 1000;
const MODEL_GROUP_PAGE_SIZE = 100;
// Each channel runs its detail read and sequential group pages together.
// Four workers therefore issue at most eight hydration RPCs at a time.
const CHANNEL_LOAD_CONCURRENCY = 4;

// Follows a cursor-paged list to its end.
async function drainPages<T>(
  fetchPage: (cursor: string) => Promise<{ items: T[]; cursor: string }>,
  signal?: AbortSignal,
  checkCurrent?: () => void,
): Promise<T[]> {
  const all: T[] = [];
  let cursor = "";
  do {
    checkCurrent?.();
    signal?.throwIfAborted();
    const page = await fetchPage(cursor);
    checkCurrent?.();
    signal?.throwIfAborted();
    all.push(...page.items);
    cursor = page.cursor;
  } while (cursor !== "");
  return all;
}

interface MinerNamesCache {
  value: Record<string, string> | null;
  expiresAt: number;
  inFlight: Promise<Record<string, string>> | null;
}

function loadMinerNames(
  cache: MinerNamesCache,
  signal: AbortSignal,
  checkCurrent: () => void,
): Promise<Record<string, string>> {
  checkCurrent();
  if (cache.value !== null && Date.now() < cache.expiresAt) return Promise.resolve(cache.value);
  if (cache.inFlight) return cache.inFlight;
  const request = drainPages(
    (cursor) =>
      fleetManagementClient
        .listMinerStateSnapshots({ pageSize: DETAIL_PAGE_SIZE, cursor }, { timeoutMs: POLL_RPC_TIMEOUT_MS, signal })
        .then((resp) => ({ items: resp.miners, cursor: resp.cursor })),
    signal,
    checkCurrent,
  )
    .catch((error: unknown) => {
      checkCurrent();
      // Firmware managers need not have miner:read. Clear names when that
      // permission is denied, and avoid retrying the denied scan every poll.
      if (error instanceof ConnectError && error.code === Code.PermissionDenied) return [];
      throw error;
    })
    .then((miners) => {
      checkCurrent();
      const names = Object.fromEntries(miners.map((miner) => [miner.deviceIdentifier, miner.name]));
      cache.value = names;
      cache.expiresAt = Date.now() + MINER_NAMES_REFRESH_INTERVAL_MS;
      return names;
    })
    .finally(() => {
      cache.inFlight = null;
    });
  cache.inFlight = request;
  return request;
}

async function loadRolloutChanges(
  pollCursor: string,
  signal: AbortSignal,
  checkCurrent: () => void,
): Promise<{ rollouts: Rollout[]; pollCursor: string }> {
  let nextPollCursor = "";
  const rollouts = await drainPages(
    async (cursor) => {
      // Keep the preceding cycle's watermark fixed until every page succeeds.
      const response = await rolloutClient.listRollouts(
        {
          pageSize: DETAIL_PAGE_SIZE,
          cursor,
          pollCursor,
          // The overview needs live work, not the fleet's entire history.
          // Its watermark still lets later unfiltered deltas capture completions.
          ...(pollCursor ? {} : { status: RolloutStatus.ACTIVE }),
        },
        { timeoutMs: POLL_RPC_TIMEOUT_MS, signal },
      );
      nextPollCursor = response.pollCursor;
      return { items: response.rollouts, cursor: response.cursor };
    },
    signal,
    checkCurrent,
  );
  return { rollouts, pollCursor: nextPollCursor };
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
  // Active baseline plus changes observed since loading. Historical reads
  // are scoped to a channel and requested only when its details are opened.
  rollouts: Rollout[];
  // Committed rollback sources whose assignment changes are not fully polled yet.
  acknowledgedRollbacks: readonly Rollout[];
  // deviceIdentifier -> display name, from fleet snapshots.
  minerNames: Record<string, string>;
  isLoading: boolean;
  // Distinguish a successfully loaded empty fleet from a failed initial read.
  hasLoaded: boolean;
  // A read failure preserves the last complete snapshot and any committed write.
  error: Error | null;
  refresh: () => Promise<void>;
  createChannel: (draft: ReleaseChannelDraft) => Promise<ReleaseChannel | undefined>;
  updateChannel: (channelId: bigint, draft: ReleaseChannelDraft) => Promise<ReleaseChannel | undefined>;
  deleteChannel: (channelId: bigint) => Promise<void>;
  // Read-only: does not touch the polled state.
  previewScope: (
    scope: ReleaseChannelScope,
    channelId?: bigint,
    signal?: AbortSignal,
  ) => Promise<PreviewReleaseChannelScopeResponse>;
  // Read-only detail lists. The server pages both; these walk every page so
  // a modal can show the whole set. Filters match observed identities
  // verbatim.
  listChannelMiners: (
    channelId: bigint,
    manufacturer?: string,
    model?: string,
    signal?: AbortSignal,
  ) => Promise<ReleaseChannelMiner[]>;
  listRolloutDevices: (rolloutId: bigint, signal?: AbortSignal) => Promise<RolloutDevice[]>;
  listChannelRollouts: (channelId: bigint, signal?: AbortSignal) => Promise<Rollout[]>;
  applyFirmware: (channelId: bigint, assignments: AssignmentDraft[]) => Promise<Rollout[]>;
  rollbackFirmware: (source: Rollout) => Promise<Rollout[]>;
  continueRollout: (rolloutId: bigint, expectedRevision: bigint) => Promise<void>;
  pauseRollout: (rolloutId: bigint, expectedRevision: bigint) => Promise<void>;
  resumeRollout: (rolloutId: bigint, expectedRevision: bigint) => Promise<void>;
  cancelRollout: (rolloutId: bigint, expectedRevision: bigint) => Promise<void>;
  retryFailedDevices: (rolloutId: bigint, expectedRevision: bigint) => Promise<Rollout | undefined>;
}

interface MutationAcknowledgment {
  rollouts: readonly Rollout[];
  rollback?: Rollout;
}

function retainAcknowledgment<T extends { rollouts: Rollout[]; acknowledgedRollbacks: Rollout[] }>(
  current: T,
  acknowledgment: MutationAcknowledgment,
): T {
  const { rollback } = acknowledgment;
  const acknowledgedRollbacks =
    rollback && !isRollbackAcknowledged(rollback, current.acknowledgedRollbacks)
      ? [...current.acknowledgedRollbacks.filter((source) => !isRollbackAcknowledged(source, [rollback])), rollback]
      : current.acknowledgedRollbacks;
  return {
    ...current,
    rollouts: mergeRollouts(current.rollouts, rollback ? [rollback] : [], acknowledgment.rollouts),
    acknowledgedRollbacks,
  };
}

// Keep hydration bounded while retaining list order and publishing only a
// complete scan. Failure cancels peers and drains them before releasing the
// refresh lock, including both branches of every started channel read.
async function loadChannels(
  channelIds: bigint[],
  controller: AbortController,
  isCurrentRequest: () => boolean,
): Promise<(ChannelView | undefined)[]> {
  const { signal } = controller;
  const checkCurrent = () => {
    if (!isCurrentRequest()) controller.abort();
    signal.throwIfAborted();
  };
  let failed = false;
  let firstError: unknown;
  const fail = (error: unknown) => {
    if (!failed) {
      failed = true;
      firstError = error;
    }
    controller.abort();
  };
  const loadChannel = async (channelId: bigint): Promise<ChannelView | undefined> => {
    checkCurrent();
    const requests = [
      rolloutClient.getReleaseChannel({ channelId }, { timeoutMs: POLL_RPC_TIMEOUT_MS, signal }),
      drainPages((cursor) => {
        checkCurrent();
        return rolloutClient
          .listReleaseChannelModelGroups(
            { channelId, pageSize: MODEL_GROUP_PAGE_SIZE, cursor },
            { timeoutMs: POLL_RPC_TIMEOUT_MS, signal },
          )
          .then((resp) => ({ items: resp.modelGroups, cursor: resp.cursor }));
      }, signal),
    ] as const;
    try {
      const [detail, modelGroups] = await Promise.all(requests);
      checkCurrent();
      return detail.channel ? { ...detail.channel, modelGroups } : undefined;
    } catch (error) {
      fail(error);
      await Promise.allSettled(requests);
      throw error;
    }
  };
  const views = new Array<ChannelView | undefined>(channelIds.length);
  let nextIndex = 0;
  await Promise.allSettled(
    Array.from({ length: Math.min(CHANNEL_LOAD_CONCURRENCY, channelIds.length) }, async () => {
      try {
        while (nextIndex < channelIds.length) {
          checkCurrent();
          const index = nextIndex++;
          views[index] = await loadChannel(channelIds[index]);
        }
      } catch (error) {
        fail(error);
      }
    }),
  );
  if (failed) throw firstError;
  checkCurrent();
  return views;
}

// Fetches release channels and rollouts, polling while mounted so firmware
// versions and update progress stay live.
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
    acknowledgedRollbacks: [] as Rollout[],
    isLoading: true,
    hasLoaded: false,
    error: null as Error | null,
  });
  const [namesSnapshot, setNamesSnapshot] = useState({
    authSessionIdentity,
    value: {} as Record<string, string>,
  });
  const inFlightRef = useRef<Promise<void> | null>(null);
  const sessionRef = useRef<object | null>(null);
  const pollControllerRef = useRef<AbortController | null>(null);
  const namesControllerRef = useRef<AbortController | null>(null);
  const rolloutSnapshotRef = useRef({
    rollouts: [] as Rollout[],
    pollCursor: "",
    acknowledgedRollbacks: [] as Rollout[],
  });
  const minerNamesCacheRef = useRef<MinerNamesCache>({ value: null, expiresAt: 0, inFlight: null });

  const isCurrentSession = useCallback(() => {
    const auth = useFleetStore.getState().auth;
    return (
      isAuthenticated &&
      auth.isAuthenticated &&
      auth.sessionGeneration === sessionGeneration &&
      auth.username === username
    );
  }, [isAuthenticated, sessionGeneration, username]);

  const refreshMinerNames = useCallback(
    async (session: object) => {
      const cache = minerNamesCacheRef.current;
      const controller = namesControllerRef.current;
      const isCurrentRequest = () => sessionRef.current === session && isCurrentSession();
      if (!controller || !isCurrentRequest() || cache.inFlight) return;
      if (cache.value !== null && Date.now() < cache.expiresAt) return;
      const checkCurrent = () => {
        if (!isCurrentRequest()) controller.abort();
        controller.signal.throwIfAborted();
      };
      try {
        const value = await loadMinerNames(cache, controller.signal, checkCurrent);
        checkCurrent();
        setNamesSnapshot({ authSessionIdentity, value });
      } catch (error) {
        // Display names enrich the UI independently of its rollout snapshot.
        // Retain complete names on transient failures, and handle each scan's
        // authentication failure once even when several refreshes overlap it.
        if (!isCurrentRequest() || controller.signal.aborted) return;
        if (error instanceof ConnectError && error.code === Code.Unauthenticated) handleAuthErrors({ error });
      }
    },
    [authSessionIdentity, handleAuthErrors, isCurrentSession],
  );

  const fetchState = useCallback(
    async (session: object) => {
      const isCurrentRequest = () => sessionRef.current === session && isCurrentSession();
      const controller = new AbortController();
      const { signal } = controller;
      pollControllerRef.current = controller;
      const checkCurrent = () => {
        if (!isCurrentRequest()) controller.abort();
        signal.throwIfAborted();
      };
      const coreRequests: Promise<unknown>[] = [];
      try {
        checkCurrent();
        const previous = rolloutSnapshotRef.current;
        const changesRequest = loadRolloutChanges(previous.pollCursor, signal, checkCurrent);
        // Telemetry/evidence can change without a header revision. Refresh
        // active rows separately; this work does not grow with history.
        const activeRequest = previous.pollCursor
          ? drainPages(
              (cursor) =>
                rolloutClient
                  .listRollouts(
                    { pageSize: DETAIL_PAGE_SIZE, cursor, status: RolloutStatus.ACTIVE },
                    { timeoutMs: POLL_RPC_TIMEOUT_MS, signal },
                  )
                  .then((resp) => ({ items: resp.rollouts, cursor: resp.cursor })),
              signal,
              checkCurrent,
            )
          : Promise.resolve([] as Rollout[]);
        coreRequests.push(changesRequest, activeRequest);
        const [changes, activeRollouts] = await Promise.all([changesRequest, activeRequest]);
        checkCurrent();
        // Read channels after rollouts, so a newly created channel cannot be
        // mistaken for a deletion when its rollout arrives in this cycle.
        const channelSummaries = await drainPages(
          (cursor) =>
            rolloutClient
              .listReleaseChannels({ pageSize: DETAIL_PAGE_SIZE, cursor }, { timeoutMs: POLL_RPC_TIMEOUT_MS, signal })
              .then((resp) => ({ items: resp.channels, cursor: resp.cursor })),
          signal,
          checkCurrent,
        );
        checkCurrent();
        // The list carries summaries; scope and groups come per channel.
        const views = await loadChannels(
          channelSummaries.map((summary) => summary.id),
          controller,
          isCurrentRequest,
        );
        checkCurrent();
        const nextChannels = views.filter((view): view is ChannelView => view !== undefined);
        const channelsById = new Map(nextChannels.map((channel) => [channel.id, channel]));
        // A successful control can update the cache while this poll is reading.
        // Merge against the latest rows so an older read cannot undo that write.
        const allRollouts = mergeRollouts(rolloutSnapshotRef.current.rollouts, [...changes.rollouts, ...activeRollouts])
          // Channel deletion cascades to rollouts without a delta tombstone.
          .filter((rollout) => channelsById.has(rollout.channelId))
          .map((rollout) => ({ ...rollout, channelName: channelsById.get(rollout.channelId)!.name }));
        const acknowledgedRollbacks = rolloutSnapshotRef.current.acknowledgedRollbacks.filter((source) => {
          // An older in-flight poll cannot acknowledge a later write. A fresh
          // complete read must confirm both the new assignment and the old
          // active runs' terminal state before their projection can be removed.
          if (!previous.acknowledgedRollbacks.includes(source)) return true;
          const channel = channelsById.get(source.channelId);
          if (!channel) return false;
          const assignmentPending = channel.modelGroups.some((group) =>
            isRollbackAcknowledged({ ...group, channelId: channel.id }, [source]),
          );
          return (
            assignmentPending ||
            allRollouts.some(
              (rollout) => rollout.status === RolloutStatus.ACTIVE && isRollbackAcknowledged(rollout, [source]),
            )
          );
        });
        // Commit the watermark with the complete UI snapshot. A failure in
        // any list or channel detail must replay the same delta on retry.
        rolloutSnapshotRef.current = { rollouts: allRollouts, pollCursor: changes.pollCursor, acknowledgedRollbacks };
        setSnapshot({
          authSessionIdentity,
          channels: nextChannels,
          rollouts: allRollouts,
          acknowledgedRollbacks,
          isLoading: false,
          hasLoaded: true,
          error: null,
        });
      } catch (error) {
        // Hold the refresh lock until all core reads settle. The session-owned
        // names scan remains reusable after a core failure, but cleanup cancels it.
        controller.abort();
        await Promise.allSettled(coreRequests);
        // A delayed 401 from the previous login must not log out its replacement.
        if (!isCurrentRequest()) return;
        const refreshError = error instanceof Error ? error : new Error("Failed to load release channels");
        setSnapshot((previous) =>
          previous.authSessionIdentity === authSessionIdentity
            ? { ...previous, isLoading: false, error: refreshError }
            : {
                authSessionIdentity,
                channels: [],
                rollouts: [],
                acknowledgedRollbacks: [],
                isLoading: false,
                hasLoaded: false,
                error: refreshError,
              },
        );
        handleAuthErrors({ error });
        throw error;
      } finally {
        if (pollControllerRef.current === controller) pollControllerRef.current = null;
      }
    },
    [authSessionIdentity, handleAuthErrors, isCurrentSession],
  );

  const refresh = useCallback(async () => {
    const session = sessionRef.current;
    if (!session || !isCurrentSession()) return;
    void refreshMinerNames(session);
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
  }, [fetchState, isCurrentSession, refreshMinerNames]);

  useEffect(() => {
    // A new login needs its own baseline and must not wait for old requests.
    const session = {};
    sessionRef.current = session;
    inFlightRef.current = null;
    rolloutSnapshotRef.current = { rollouts: [], pollCursor: "", acknowledgedRollbacks: [] };
    // Keep each login's completed and pending name scans isolated.
    minerNamesCacheRef.current = { value: null, expiresAt: 0, inFlight: null };
    namesControllerRef.current = new AbortController();
    const poll = () => {
      void refreshMinerNames(session);
      // Timer ticks never queue work behind an existing refresh.
      if (inFlightRef.current) return;
      refresh().catch((error) => console.error("Failed to refresh release channels", error));
    };
    poll();
    const timer = setInterval(poll, POLL_INTERVAL_MS);
    return () => {
      clearInterval(timer);
      sessionRef.current = null;
      pollControllerRef.current?.abort();
      pollControllerRef.current = null;
      namesControllerRef.current?.abort();
      namesControllerRef.current = null;
    };
  }, [refresh, refreshMinerNames]);

  const withAuthErrors = useCallback(
    async <T>(request: () => Promise<T>, signal?: AbortSignal): Promise<T> => {
      signal?.throwIfAborted();
      const session = sessionRef.current;
      if (!session || !isCurrentSession()) {
        throw new Error("Your session changed. Refresh the page before trying again.");
      }
      try {
        return await request();
      } catch (error) {
        signal?.throwIfAborted();
        // Direct actions need the same logout path as polling. A late failure
        // from a previous login or an unmounted hook must not end a new session.
        if (sessionRef.current === session && isCurrentSession()) handleAuthErrors({ error });
        throw error;
      }
    },
    [handleAuthErrors, isCurrentSession],
  );

  const mutate = useCallback(
    async <T>(request: () => Promise<T>, acknowledge?: (response: T) => MutationAcknowledgment): Promise<T> => {
      const session = sessionRef.current;
      const response = await withAuthErrors(request);
      if (session && sessionRef.current === session && isCurrentSession()) {
        const acknowledgment = acknowledge?.(response);
        if (acknowledgment) {
          // Retain the server's committed state even if the follow-up read fails.
          // Keep the delta cursor unchanged so subsequent polls still replay all
          // changes, and keep any newer revision already delivered by a poll.
          rolloutSnapshotRef.current = retainAcknowledgment(rolloutSnapshotRef.current, acknowledgment);
          setSnapshot((previous) =>
            sessionRef.current === session && isCurrentSession() && previous.authSessionIdentity === authSessionIdentity
              ? retainAcknowledgment(previous, acknowledgment)
              : previous,
          );
        }
        // The write committed. Keep read failures in polling error state instead
        // of inviting a duplicate write; always return the acknowledged response.
        await refresh().catch(() => undefined);
      }
      return response;
    },
    [authSessionIdentity, isCurrentSession, refresh, withAuthErrors],
  );

  const createChannel = useCallback(
    (draft: ReleaseChannelDraft) =>
      mutate(() =>
        rolloutClient.createReleaseChannel({ ...draft, behavior: rolloutBehaviorForRequest(draft.behavior) }),
      ).then((response) => response.channel),
    [mutate],
  );

  const updateChannel = useCallback(
    (channelId: bigint, draft: ReleaseChannelDraft) =>
      mutate(() =>
        rolloutClient.updateReleaseChannel({
          channelId,
          ...draft,
          behavior: rolloutBehaviorForRequest(draft.behavior),
        }),
      ).then((response) => response.channel),
    [mutate],
  );

  const deleteChannel = useCallback(
    async (channelId: bigint) => {
      await mutate(() => rolloutClient.deleteReleaseChannel({ channelId }));
    },
    [mutate],
  );

  const previewScope = useCallback(
    async (scope: ReleaseChannelScope, channelId?: bigint, signal?: AbortSignal) => {
      const session = sessionRef.current;
      const response = await withAuthErrors(
        () =>
          rolloutClient.previewReleaseChannelScope(
            { scope, channelId: channelId ?? 0n },
            { timeoutMs: DETAIL_RPC_TIMEOUT_MS, signal },
          ),
        signal,
      );
      // A completed read can still belong to an abandoned editor or login.
      signal?.throwIfAborted();
      if (sessionRef.current !== session || !isCurrentSession()) {
        throw new Error("Your session changed. Refresh the page before trying again.");
      }
      return response;
    },
    [isCurrentSession, withAuthErrors],
  );

  const listChannelMiners = useCallback(
    (channelId: bigint, manufacturer?: string, model?: string, signal?: AbortSignal) =>
      drainPages(
        (cursor) =>
          withAuthErrors(
            () =>
              rolloutClient.listReleaseChannelMiners(
                {
                  channelId,
                  manufacturer: manufacturer ?? "",
                  model: model ?? "",
                  pageSize: DETAIL_PAGE_SIZE,
                  cursor,
                },
                { timeoutMs: DETAIL_RPC_TIMEOUT_MS, signal },
              ),
            signal,
          ).then((resp) => ({ items: resp.miners, cursor: resp.cursor })),
        signal,
      ),
    [withAuthErrors],
  );

  const listChannelRollouts = useCallback(
    (channelId: bigint, signal?: AbortSignal) => {
      const session = sessionRef.current;
      const checkCurrent = () => {
        signal?.throwIfAborted();
        if (!session || sessionRef.current !== session || !isCurrentSession()) {
          throw new Error("Your session changed. Refresh the page before trying again.");
        }
      };
      return drainPages(
        (cursor) =>
          withAuthErrors(
            () =>
              rolloutClient.listRollouts(
                { channelId, pageSize: DETAIL_PAGE_SIZE, cursor },
                { timeoutMs: DETAIL_RPC_TIMEOUT_MS, signal },
              ),
            signal,
          ).then((resp) => ({ items: resp.rollouts, cursor: resp.cursor })),
        signal,
        checkCurrent,
      );
    },
    [isCurrentSession, withAuthErrors],
  );

  const listRolloutDevices = useCallback(
    (rolloutId: bigint, signal?: AbortSignal) =>
      drainPages(
        (cursor) =>
          withAuthErrors(
            () =>
              rolloutClient.listRolloutDevices(
                { rolloutId, pageSize: DETAIL_PAGE_SIZE, cursor },
                { timeoutMs: DETAIL_RPC_TIMEOUT_MS, signal },
              ),
            signal,
          ).then((resp) => ({ items: resp.devices, cursor: resp.cursor })),
        signal,
      ),
    [withAuthErrors],
  );

  const applyFirmware = useCallback(
    (channelId: bigint, assignments: AssignmentDraft[]) =>
      mutate(
        () =>
          rolloutClient.applyReleaseChannelFirmware({
            channelId,
            assignments: assignments.map(({ manufacturer, model, firmwareFileId }) => ({
              manufacturer,
              model,
              firmwareFileId,
            })),
          }),
        (response) => ({ rollouts: response.startedRollouts }),
      ).then((response) => response.startedRollouts),
    [mutate],
  );

  const rollbackFirmware = useCallback(
    async (source: Rollout) => {
      const request = rolloutControlRequest(source.id, source.revision);
      const response = await mutate(
        () => rolloutClient.rollbackReleaseChannelFirmware(request),
        (response) => ({ rollouts: response.startedRollouts, rollback: source }),
      );
      return response.startedRollouts;
    },
    [mutate],
  );

  const retryFailedDevices = useCallback(
    async (rolloutId: bigint, expectedRevision: bigint) => {
      const request = rolloutControlRequest(rolloutId, expectedRevision);
      const response = await mutate(
        () => rolloutClient.retryFailedRolloutDevices(request),
        (response) => ({ rollouts: response.rollout ? [response.rollout] : [] }),
      );
      return response.rollout;
    },
    [mutate],
  );

  const controls = useMemo(() => {
    const control =
      (request: (input: ReturnType<typeof rolloutControlRequest>) => Promise<{ rollout?: Rollout }>) =>
      async (rolloutId: bigint, expectedRevision: bigint) => {
        const input = rolloutControlRequest(rolloutId, expectedRevision);
        await mutate(
          () => request(input),
          (response) => ({ rollouts: response.rollout ? [response.rollout] : [] }),
        );
      };
    return {
      continueRollout: control((input) => rolloutClient.continueRollout(input)),
      pauseRollout: control((input) => rolloutClient.pauseRollout(input)),
      resumeRollout: control((input) => rolloutClient.resumeRollout(input)),
      cancelRollout: control((input) => rolloutClient.cancelRollout(input)),
    };
  }, [mutate]);

  const hasCurrentSnapshot = isAuthenticated && snapshot.authSessionIdentity === authSessionIdentity;
  const view = useMemo(() => {
    if (!hasCurrentSnapshot) return { channels: [], rollouts: [], acknowledgedRollbacks: [] };
    const sources = snapshot.acknowledgedRollbacks;
    const projectedRollouts = snapshot.rollouts.map((rollout) =>
      acknowledgeRollout(rollout, sources, snapshot.channels),
    );
    const rollouts = projectedRollouts.every((rollout, index) => rollout === snapshot.rollouts[index])
      ? snapshot.rollouts
      : projectedRollouts;
    if (sources.length === 0) return { ...snapshot, rollouts };
    return {
      acknowledgedRollbacks: sources,
      rollouts,
      channels: snapshot.channels.map((channel) => {
        const modelGroups = channel.modelGroups.map((group) =>
          isRollbackAcknowledged({ ...group, channelId: channel.id }, sources)
            ? { ...group, rollbackPending: true }
            : group,
        );
        return modelGroups.some((group) => group.rollbackPending) ? { ...channel, modelGroups } : channel;
      }),
    };
  }, [hasCurrentSnapshot, snapshot]);
  return {
    channels: view.channels,
    rollouts: view.rollouts,
    acknowledgedRollbacks: view.acknowledgedRollbacks,
    minerNames: isAuthenticated && namesSnapshot.authSessionIdentity === authSessionIdentity ? namesSnapshot.value : {},
    isLoading: isAuthenticated && (!hasCurrentSnapshot || snapshot.isLoading),
    hasLoaded: hasCurrentSnapshot && snapshot.hasLoaded,
    error: hasCurrentSnapshot ? snapshot.error : null,
    refresh,
    createChannel,
    updateChannel,
    deleteChannel,
    previewScope,
    listChannelMiners,
    listChannelRollouts,
    listRolloutDevices,
    applyFirmware,
    rollbackFirmware,
    ...controls,
    retryFailedDevices,
  };
}
