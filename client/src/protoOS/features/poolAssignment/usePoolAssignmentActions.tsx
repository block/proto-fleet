import { useEffect, useId, useState } from "react";
import { readPoolAttempts, subscribeToPoolAttempts } from "./attemptStorage";
import PoolAssignmentDialog from "./PoolAssignmentDialog";
import { hasWorkerNameUpdate, useWorkerNameUpdateOwner } from "./workerNameUpdates";
import type { PoolAssignmentAttempt, PoolAssignmentTarget } from "@/protoOS/api/adapters/poolAssignment";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";
import Button from "@/shared/components/Button";
import type { RowAction } from "@/shared/components/RowActionsMenu";

export function usePoolAssignmentActions({
  onPoolsChanged,
  showRecovery = false,
}: { onPoolsChanged?: () => void; showRecovery?: boolean } = {}) {
  const { adapter, isFleetHosted } = useMinerHosting();
  const identity = useMinerStore((state) => state.systemInfo.data?.identity);
  const generation = useMinerStore((state) => state.connectionGeneration);
  const [request, setRequest] = useState<{
    target: PoolAssignmentTarget;
    generation: number;
    openStatus?: boolean;
    workerNames?: boolean;
  }>();
  const service = !isFleetHosted ? adapter?.poolAssignment : undefined;
  const controllerId = identity?.deviceId;
  const owner = useId();
  const workerNameUpdateOwner = useWorkerNameUpdateOwner(controllerId);
  const recoveryController = service && showRecovery ? controllerId : undefined;
  const [recovery, setRecovery] = useState<{ controller: string; attempt?: PoolAssignmentAttempt }>();
  useEffect(() => {
    if (!recoveryController) return;
    const update = () =>
      setRecovery({ controller: recoveryController, attempt: readPoolAttempts(recoveryController)[0] });
    const unsubscribe = subscribeToPoolAttempts(recoveryController, update);
    update();
    return unsubscribe;
  }, [recoveryController]);
  const tracked = recovery?.controller === recoveryController ? recovery?.attempt : undefined;
  const available = Boolean(service && controllerId);
  const openAssignment = (target: PoolAssignmentTarget | undefined, workerNames: boolean) => {
    if (target?.macs.length && controllerId && !hasWorkerNameUpdate(controllerId))
      setRequest({ target, generation, workerNames });
  };
  const action = (target?: PoolAssignmentTarget, scope: "board" | "module" = "board"): RowAction[] =>
    available
      ? [
          {
            label: "Edit pool",
            testId: "set-board-pools",
            disabled: !target?.macs.length || workerNameUpdateOwner !== undefined,
            onClick: () => openAssignment(target, false),
          },
          {
            label: scope === "module" || (target?.macs.length ?? 0) > 1 ? "Update worker names" : "Update worker name",
            testId: "update-board-worker-name",
            disabled: !target?.macs.length || workerNameUpdateOwner !== undefined,
            onClick: () => openAssignment(target, true),
          },
        ]
      : [];
  const activeRequest = request?.generation === generation ? request : undefined;
  return {
    available,
    action,
    open: Boolean(activeRequest) && workerNameUpdateOwner !== owner,
    recoveryButton:
      available && tracked && workerNameUpdateOwner === undefined ? (
        <Button
          className="mb-4"
          variant="secondary"
          onClick={() => setRequest({ target: { macs: tracked.macs }, generation, openStatus: true })}
        >
          Pool assignment status
        </Button>
      ) : null,
    dialog:
      activeRequest && service && controllerId ? (
        <PoolAssignmentDialog
          key={controllerId}
          service={service}
          controllerId={controllerId}
          target={activeRequest.target}
          openStatus={activeRequest.openStatus}
          workerNames={activeRequest.workerNames}
          workerNameUpdateOwner={owner}
          onConfigurationSaved={onPoolsChanged}
          onAssignmentSaved={onPoolsChanged}
          onDismiss={() => setRequest(undefined)}
        />
      ) : null,
  };
}
