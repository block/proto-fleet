import { useState } from "react";
import type { MinerAdapter, ModulePowerFeedInspection, ModulePowerFeedTarget } from "@/protoOS/api/adapters/types";
import type { ModuleNode, TankNode } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { useMountedCallback } from "@/shared/hooks/useMountedCallback";
import { usePoll } from "@/shared/hooks/usePoll";

/** The module's controller position and the ID the store holds for it. */
export function modulePowerFeedTarget(module: ModuleNode, tank: TankNode): ModulePowerFeedTarget | undefined {
  const moduleNumber = module.location?.moduleSlot;
  if (tank.slot === undefined || moduleNumber === undefined) return undefined;
  return { tank: tank.slot, module: moduleNumber, moduleId: module.id };
}

/**
 * Reads one module's feed when enabled and again on refresh, like Fleet's
 * module page. It never polls: each read fetches the whole controller map.
 * A failed read replaces the last reading.
 */
export function useModulePowerFeed(
  read: MinerAdapter["getModulePowerFeed"],
  target: ModulePowerFeedTarget | undefined,
  enabled: boolean,
) {
  const authRetry = useAuthRetry();
  const [revision, setRevision] = useState(0);
  const key = target ? `${target.tank}:${target.module}:${target.moduleId}` : "";
  const active = enabled && !!read && !!target;
  const [result, setResult] = useState<{
    key: string;
    revision: number;
    data?: ModulePowerFeedInspection;
    error?: string;
  }>();
  usePoll({
    enabled: active,
    poll: false,
    params: `${key}:${revision}`,
    fetchData: (signal) => {
      if (!read || !target) return;
      // An expired token is refreshed and the read repeated once.
      return authRetry({
        request: (params) => read(target, params),
        signal,
        onSuccess: (data) => {
          if (!signal.aborted) setResult({ key, revision, data });
        },
        onError: (error) => {
          // Connect reports a code with no text as an empty message.
          const message = error?.error?.message || (error instanceof Error ? error.message : "");
          setResult({ key, revision, error: message.trim() || "The controller did not answer." });
        },
      });
    },
  });
  const current = active && result?.key === key ? result : undefined;
  return {
    data: current?.data,
    error: current?.error,
    loading: active && !current,
    refreshing: !!current && current.revision !== revision,
    refresh: useMountedCallback(() => setRevision((value) => value + 1)),
  };
}
