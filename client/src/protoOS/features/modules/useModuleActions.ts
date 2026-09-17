import { useEffect, useRef, useState } from "react";

import { moduleActions, moduleActionTarget, type ModuleActionTarget } from "./moduleActions";
import { buildModuleRows, type ModuleListItem } from "./moduleListModel";
import type { HashboardAction, HashboardActionResult } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import type { Topology } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
import type { RowAction } from "@/shared/components/RowActionsMenu";

interface Request {
  action: HashboardAction;
  target: ModuleActionTarget;
  generation: number;
}
export interface ModuleActionOutcome {
  result?: HashboardActionResult;
  error?: string;
  refreshFailed?: boolean;
}

// Bound the caller's wait even when a transport or authentication refresh does
// not settle on abort. The same signal also prevents a late auth retry from
// issuing another command after the outcome has become unknown.
const withDeadline = async <T>(request: () => Promise<T>, controller: AbortController, timeoutMs: number) => {
  const { signal } = controller;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let onAbort: () => void = () => {};
  try {
    return await new Promise<T>((resolve, reject) => {
      onAbort = () => reject(signal.reason);
      if (signal.aborted) {
        onAbort();
        return;
      }
      signal.addEventListener("abort", onAbort, { once: true });
      timer = setTimeout(() => controller.abort(new DOMException("Request timed out", "TimeoutError")), timeoutMs);
      request().then(resolve, reject);
    });
  } finally {
    clearTimeout(timer);
    signal.removeEventListener("abort", onAbort);
  }
};

export const useModuleActions = (rows: ModuleListItem[], topology?: Topology) => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const generation = useMinerStore((state) => state.connectionGeneration);
  const [request, setRequest] = useState<Request>();
  const [pending, setPending] = useState(false);
  const [outcome, setOutcome] = useState<ModuleActionOutcome>();
  const inFlight = useRef(false);
  const controller = useRef<AbortController | undefined>(undefined);
  useEffect(
    () => () => {
      controller.current?.abort();
      controller.current = undefined;
    },
    [adapter, generation],
  );
  const activeRequest = request?.generation === generation ? request : undefined;

  const actionsFor = (ids: string[]): RowAction[] => {
    const target = topology?.kind === "container" ? moduleActionTarget(rows, ids, topology) : undefined;
    return moduleActions.map(({ id, label }) => ({
      label,
      testId: `module-action-${id}`,
      disabled:
        !adapter?.runHashboardAction ||
        !target ||
        pending ||
        ((id === "start" || id === "stop") &&
          ids.some(
            (moduleId) =>
              rows.find((row) => row.id === moduleId)?.module.capabilities.commands.startStop !== "supported",
          )),
      showGroupDivider: id === "stop" || id === "identify",
      onClick: () => {
        if (!target || inFlight.current) return;
        setOutcome(undefined);
        setRequest({ action: id, target, generation });
      },
    }));
  };

  const confirm = async () => {
    if (!activeRequest || !adapter?.runHashboardAction || inFlight.current) return;
    const { target, action } = activeRequest;
    const targetIsCurrent = () => {
      const state = useMinerStore.getState();
      const currentTopology = state.hardware.topology;
      const currentRows =
        currentTopology?.kind === "container" ? buildModuleRows(currentTopology, state.telemetry) : [];
      const current =
        currentTopology?.kind === "container"
          ? moduleActionTarget(currentRows, target.moduleIds, currentTopology)
          : undefined;
      return Boolean(
        current &&
        JSON.stringify(current.identifiers) === JSON.stringify(target.identifiers) &&
        !(
          (action === "start" || action === "stop") &&
          currentRows.some(
            (row) => target.moduleIds.includes(row.id) && row.module.capabilities.commands.startStop !== "supported",
          )
        ),
      );
    };
    const changedMessage = "The selected modules changed. Close this dialog and select them again.";
    if (!targetIsCurrent()) {
      setOutcome({ error: changedMessage });
      return;
    }
    const abort = new AbortController();
    controller.current = abort;
    const isActive = () => controller.current === abort && useMinerStore.getState().connectionGeneration === generation;
    const isCurrent = () => !abort.signal.aborted && isActive();
    inFlight.current = true;
    setPending(true);
    let commanded = false;
    let commandResult: HashboardActionResult | undefined;
    try {
      await withDeadline(
        () =>
          authRetry({
            signal: abort.signal,
            shouldRetry: () => !commanded && isCurrent() && targetIsCurrent(),
            request: async (params) => {
              if (!isCurrent()) return undefined;
              if (!targetIsCurrent()) throw new Error(changedMessage);
              const result = await adapter.runHashboardAction!(action, target.identifiers, params);
              commanded = true;
              return result;
            },
            onSuccess: (result) => {
              if (!isCurrent() || !result) return;
              commandResult = result;
            },
            onError: () => {
              if (isCurrent())
                setOutcome({
                  error: targetIsCurrent()
                    ? "Could not complete the command. Check module status before trying again."
                    : changedMessage,
                });
            },
          }),
        abort,
        30_000,
      );
      if (!isCurrent() || !commandResult) return;
      const result = commandResult;
      setOutcome({ result });
      const accept = beginStoreWrite("telemetry:current:hashboard,miner");
      try {
        const telemetry = await withDeadline(
          () => adapter.getCurrentTelemetry(["hashboard", "miner"], { signal: abort.signal }),
          abort,
          15_000,
        );
        if (isCurrent() && accept()) useMinerStore.getState().telemetry.mergeTelemetry(telemetry, accept.requestId);
      } catch {
        if (isActive()) setOutcome({ result, refreshFailed: true });
      }
    } catch {
      if (isActive())
        setOutcome({
          error: "The command timed out and its outcome is unknown. Check module status before trying again.",
        });
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  };

  return {
    actionsFor,
    request: activeRequest,
    pending,
    outcome,
    confirm,
    dismiss: () => {
      if (!inFlight.current) setRequest(undefined);
    },
  };
};
