import { useCallback, useState } from "react";
import { fleetInventoryClient } from "./clients";
import type { ModuleHashboards } from "./generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "./getErrorMessage";
import { usePoll } from "@/shared/hooks/usePoll";

export type ModuleHashboardResult = { data?: ModuleHashboards; error?: string };

// Poll only expanded, visible modules. The query key and AbortSignal prevent
// cancelled responses from crossing navigation or attaching to another module.
export function useModuleHashboards(moduleIds: bigint[], scope: string) {
  const ids = [...new Set(moduleIds.map(String))].sort();
  const key = JSON.stringify([scope, ids]);
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{ scope: string; modules: Record<string, ModuleHashboardResult> }>({
    scope,
    modules: {},
  });
  const [previousKey, setPreviousKey] = useState(key);
  if (previousKey !== key) {
    setPreviousKey(key);
    setResult({
      scope,
      modules:
        result.scope === scope
          ? Object.fromEntries(Object.entries(result.modules).filter(([id]) => ids.includes(id)))
          : {},
    });
  }
  usePoll({
    enabled: ids.length > 0,
    poll: true,
    params: `${key}:${revision}`,
    fetchData: async (signal) => {
      const modules: Record<string, ModuleHashboardResult> = {};
      for (let start = 0; start < ids.length; start += 100) {
        if (signal.aborted) return;
        const batch = ids.slice(start, start + 100);
        try {
          const response = await fleetInventoryClient.getModuleHashboards(
            { moduleIds: batch.map(BigInt) },
            { signal, timeoutMs: 10_000 },
          );
          for (const id of batch) {
            const data = response.modules.find((module) => String(module.moduleId) === id);
            modules[id] = data ? { data } : { error: "Hashboards unavailable" };
          }
        } catch (error) {
          for (const id of batch) modules[id] = { error: getErrorMessage(error) };
        }
      }
      if (!signal.aborted) setResult({ scope, modules });
    },
  });
  const retry = useCallback(() => setRevision((value) => value + 1), [setRevision]);
  return { modules: result.scope === scope ? result.modules : {}, retry };
}
