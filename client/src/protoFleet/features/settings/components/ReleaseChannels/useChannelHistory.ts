import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useFleetStore, useIsAuthenticated, useSessionGeneration, useUsername } from "@/protoFleet/store";

export type ChannelHistoryState = { status: "loading" | "ready" | "error"; error?: string };
type HistoryEntry = ChannelHistoryState & { rollouts?: Rollout[] };
type HistoryLoader = (channelId: bigint, signal?: AbortSignal) => Promise<Rollout[]>;

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
  const entries = useRef(new Map<bigint, HistoryEntry>());
  const pending = useRef(new Map<bigint, AbortController>());
  const session = useRef<object | null>(null);
  const [retryAttempt, setRetryAttempt] = useState(0);
  const [snapshot, setSnapshot] = useState({ identity, loader: listChannelRollouts, entries: entries.current });

  useEffect(() => {
    session.current = {};
    const controllers = pending.current;
    entries.current = new Map();
    setSnapshot({ identity, loader: listChannelRollouts, entries: entries.current });
    return () => {
      session.current = null;
      for (const controller of controllers.values()) controller.abort();
      controllers.clear();
    };
  }, [identity, listChannelRollouts]);

  useEffect(() => {
    const token = session.current;
    const isCurrent = () => {
      const auth = useFleetStore.getState().auth;
      return (
        token !== null &&
        session.current === token &&
        isAuthenticated &&
        auth.isAuthenticated &&
        auth.username === username &&
        auth.sessionGeneration === sessionGeneration
      );
    };
    const publish = () => {
      if (isCurrent()) setSnapshot({ identity, loader: listChannelRollouts, entries: new Map(entries.current) });
    };
    let changed = false;
    for (const [id, controller] of pending.current) {
      if (requested.has(id)) continue;
      controller.abort();
      pending.current.delete(id);
      if (entries.current.get(id)?.status === "loading") entries.current.delete(id);
      changed = true;
    }
    if (!isCurrent()) return;
    for (const id of requested) {
      if (entries.current.has(id)) continue;
      const controller = new AbortController();
      pending.current.set(id, controller);
      entries.current.set(id, { status: "loading" });
      changed = true;
      const isCurrentRequest = () =>
        isCurrent() && !controller.signal.aborted && pending.current.get(id) === controller;
      listChannelRollouts(id, controller.signal)
        .then((history) => {
          if (!isCurrentRequest()) return;
          entries.current.set(id, { status: "ready", rollouts: history.filter((rollout) => rollout.channelId === id) });
          publish();
        })
        .catch((error: unknown) => {
          if (!isCurrentRequest()) return;
          entries.current.set(id, {
            status: "error",
            error: error instanceof Error && error.message ? error.message : "Couldn't load update history",
          });
          publish();
        })
        .finally(() => {
          if (pending.current.get(id) === controller) pending.current.delete(id);
        });
    }
    if (changed) publish();
  }, [requested, identity, isAuthenticated, username, sessionGeneration, listChannelRollouts, retryAttempt]);

  const retry = useCallback(
    (id: bigint) => {
      if (!requested.has(id) || entries.current.get(id)?.status !== "error") return;
      entries.current.delete(id);
      setRetryAttempt((attempt) => attempt + 1);
    },
    [requested],
  );
  const currentEntries =
    snapshot.identity === identity && snapshot.loader === listChannelRollouts ? snapshot.entries : undefined;
  const states = new Map<bigint, ChannelHistoryState>(currentEntries);
  for (const id of requested) if (!states.has(id)) states.set(id, { status: "loading" });
  const merged = new Map<bigint, Rollout>();
  for (const entry of currentEntries?.values() ?? []) {
    for (const rollout of entry.rollouts ?? []) {
      const previous = merged.get(rollout.id);
      if (!previous || rollout.revision > previous.revision) merged.set(rollout.id, rollout);
    }
  }
  for (const rollout of rollouts) {
    const previous = merged.get(rollout.id);
    if (!previous || rollout.revision >= previous.revision) merged.set(rollout.id, rollout);
  }
  const mergedRollouts = [...merged.values()].sort((a, b) => {
    const byCreated = (b.createdAt ? timestampMs(b.createdAt) : 0) - (a.createdAt ? timestampMs(a.createdAt) : 0);
    return byCreated || (a.id === b.id ? 0 : a.id < b.id ? 1 : -1);
  });
  return { rollouts: mergedRollouts, states, retry };
}
