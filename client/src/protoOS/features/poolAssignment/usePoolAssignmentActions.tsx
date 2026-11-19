import { useEffect, useState } from "react";
import { readPoolAttempts, subscribeToPoolAttempts } from "./attemptStorage";
import PoolAssignmentDialog from "./PoolAssignmentDialog";
import type { PoolAssignmentAttempt, PoolAssignmentTarget } from "@/protoOS/api/adapters/poolAssignment";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";
import Button from "@/shared/components/Button";
import type { RowAction } from "@/shared/components/RowActionsMenu";

export function usePoolAssignmentActions(onPoolsChanged?: () => void) {
  const { adapter, isFleetHosted } = useMinerHosting();
  const identity = useMinerStore((state) => state.systemInfo.data?.identity);
  const generation = useMinerStore((state) => state.connectionGeneration);
  const [request, setRequest] = useState<{
    target: PoolAssignmentTarget;
    generation: number;
    selectedCount?: number;
    openStatus?: boolean;
  }>();
  const service = !isFleetHosted ? adapter?.poolAssignment : undefined;
  const controllerId = identity?.deviceId;
  const recoveryController = service ? controllerId : undefined;
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
  const action = (target?: PoolAssignmentTarget, selectedCount?: number): RowAction[] =>
    available
      ? [
          {
            label: "Edit pool",
            testId: "set-board-pools",
            disabled: !target?.macs.length,
            onClick: () => {
              if (target?.macs.length) setRequest({ target, generation, selectedCount });
            },
          },
        ]
      : [];
  return {
    available,
    action,
    open: request?.generation === generation,
    recoveryButton:
      available && tracked ? (
        <Button
          className="mb-4"
          variant="secondary"
          onClick={() => setRequest({ target: { macs: tracked.macs }, generation, openStatus: true })}
        >
          Pool assignment status
        </Button>
      ) : null,
    dialog:
      request?.generation === generation && service && controllerId ? (
        <PoolAssignmentDialog
          key={controllerId}
          service={service}
          controllerId={controllerId}
          target={request.target}
          selectedCount={request.selectedCount}
          openStatus={request.openStatus}
          onConfigurationSaved={onPoolsChanged}
          onAssignmentSaved={onPoolsChanged}
          onDismiss={() => setRequest(undefined)}
        />
      ) : null,
  };
}
