import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { mergeRollouts } from "@/protoFleet/api/rolloutSnapshots";
import { useFleetStore, useIsAuthenticated, useSessionGeneration, useUsername } from "@/protoFleet/store";

export type ChannelHistoryState = { status: "loading" | "ready" | "error"; error?: string };
type HistoryEntry = ChannelHistoryState & { rollouts?: Rollout[] };
type HistoryLoader = (channelId: bigint, signal?: AbortSignal) => Promise<Rollout[]>;

const HISTORY_LOAD_CONCURRENCY = 4;

// History belongs to the surfaces that request it. Completed scans remain cached
// for this login; current polling rows overlay them without losing older outcomes.
export function useChannelHistory({
  channelIds,
  rollouts,
  listChannelRollouts,
}: {
  channelIds: readonly bigint[];
  rollouts: Rollout[];
  listChannelRollouts: HistoryLoader;
}) {
  const username = useUsername();
  const sessionGeneration = useSessionGeneration();
  const isAuthenticated = useIsAuthenticated();
  const identity = JSON.stringify([username, sessionGeneration, isAuthenticated]);
  const requestedKey = [...new Set(channelIds.map(String))].sort().join(",");
  const requested = useMemo(() => new Set(requestedKey ? requestedKey.split(",").map(BigInt) : []), [requestedKey]);
  const controls = useRef<{
    request: (ids: ReadonlySet<bigint>) => void;
    retry: (id: bigint) => void;
  } | null>(null);
  const [snapshot, setSnapshot] = useState({
    identity,
    loader: listChannelRollouts,
    entries: new Map<bigint, HistoryEntry>(),
  });

  useEffect(() => {
    let current = true;
    let demanded: ReadonlySet<bigint> = new Set();
    const entries = new Map<bigint, HistoryEntry>();
    const pending = new Map<bigint, AbortController>();
    const isCurrent = () => {
      const auth = useFleetStore.getState().auth;
      return (
        current &&
        isAuthenticated &&
        auth.isAuthenticated &&
        auth.username === username &&
        auth.sessionGeneration === sessionGeneration
      );
    };
    const publish = () => {
      if (isCurrent()) setSnapshot({ identity, loader: listChannelRollouts, entries: new Map(entries) });
    };
    const load = async (id: bigint, controller: AbortController) => {
      const isCurrentRequest = () => isCurrent() && !controller.signal.aborted && demanded.has(id);
      try {
        const history = await listChannelRollouts(id, controller.signal);
        if (!isCurrentRequest()) return;
        entries.set(id, { status: "ready", rollouts: history.filter((rollout) => rollout.channelId === id) });
      } catch (error) {
        if (!isCurrentRequest()) return;
        entries.set(id, {
          status: "error",
          error: error instanceof Error && error.message ? error.message : "Couldn't load update history",
        });
      }
      publish();
    };
    const pump = () => {
      // Demand without a cached entry or active scan is the queue. Always read
      // the latest demand, including after a scan finishes or a retry is queued.
      for (const id of demanded) {
        if (!isCurrent() || pending.size >= HISTORY_LOAD_CONCURRENCY) return;
        if (entries.has(id) || pending.has(id)) continue;
        const controller = new AbortController();
        pending.set(id, controller);
        entries.set(id, { status: "loading" });
        void load(id, controller).finally(() => {
          pending.delete(id);
          pump();
        });
      }
    };
    controls.current = {
      request: (ids) => {
        demanded = ids;
        for (const [id, controller] of pending) {
          if (demanded.has(id)) continue;
          controller.abort();
          // Keep its slot until the transport settles; closing and reopening
          // channels must not create overlapping scans beyond the limit.
          if (entries.get(id)?.status === "loading") entries.delete(id);
        }
        pump();
        publish();
      },
      retry: (id) => {
        if (!isCurrent() || !demanded.has(id) || entries.get(id)?.status !== "error") return;
        entries.delete(id);
        pump();
        publish();
      },
    };
    return () => {
      current = false;
      controls.current = null;
      for (const controller of pending.values()) controller.abort();
    };
  }, [identity, isAuthenticated, username, sessionGeneration, listChannelRollouts]);

  useEffect(() => {
    controls.current?.request(requested);
  }, [requested, identity, listChannelRollouts]);

  const retry = useCallback((id: bigint) => controls.current?.retry(id), []);
  const currentEntries =
    snapshot.identity === identity && snapshot.loader === listChannelRollouts ? snapshot.entries : undefined;
  const states = new Map<bigint, ChannelHistoryState>(currentEntries);
  for (const id of requested) if (!states.has(id)) states.set(id, { status: "loading" });
  const history = [...(currentEntries?.values() ?? [])].flatMap((entry) => entry.rollouts ?? []);
  return { rollouts: mergeRollouts(history, rollouts), states, retry };
}
