import { useEffect, useRef, useState } from "react";
import BoardIdentifyDialog from "./BoardIdentifyDialog";
import EditHashboardPoolDialog from "./EditHashboardPoolDialog";
import MiningDialog from "./MiningDialog";
import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import { canEditHashboardPools, useMiningGroupState } from "./useMiningGroupState";
import { containersClient } from "@/protoFleet/api/clients";
import type { GetModuleMiningOptionsResponse } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { WorkerNameDialog } from "@/protoFleet/features/poolConfigurations/WorkerNameDialog";
import { usePermissions } from "@/protoFleet/store";
import { ChevronDown, LEDIndicator, MiningPools, Play, Power } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import RowActionsMenu from "@/shared/components/RowActionsMenu";
import { useMountedCallback } from "@/shared/hooks/useMountedCallback";

export default function MiningActionsMenu({
  items,
  kind,
  onRefresh,
  presentation = "row",
  onActionStart,
  onActionComplete,
  unavailable = false,
}: {
  items: InventoryItem[];
  kind: "tank" | "module";
  onRefresh?: () => void;
  presentation?: "row" | "bulk" | "header";
  onActionStart?: () => void;
  onActionComplete?: () => void;
  unavailable?: boolean;
}) {
  const permissions = usePermissions();
  const [workersOpen, setWorkersOpen] = useState(false);
  const canRename =
    kind === "module" &&
    hasMiningGroupPermissions(permissions, "pools") &&
    permissions.includes("miner:update_worker_names") &&
    permissions.includes("miner:read") &&
    permissions.includes("pool:read");
  const [poolsOpen, setPoolsOpen] = useState(false);
  const [identify, setIdentify] = useState<boolean>();
  const containerRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const canEditPools =
    kind === "module" && hasMiningGroupPermissions(permissions, "pools") && permissions.includes("pool:read");
  const canIdentify = kind === "module" && permissions.includes("miner:blink_led");
  const oneContainer = new Set(items.map((item) => item.deviceIdentifier)).size === 1;
  const { state: groups, isFetching } = useMiningGroupState(
    oneContainer ? items[0].deviceIdentifier : "",
    open && (canEditPools || canRename),
  );
  const [result, setResult] = useState<{
    key: string;
    options?: Pick<GetModuleMiningOptionsResponse, "canStart" | "canStop" | "unavailableReason">;
    error?: string;
  }>();
  const [previousUnavailable, setPreviousUnavailable] = useState(unavailable);
  if (previousUnavailable !== unavailable) {
    setPreviousUnavailable(unavailable);
    setResult(undefined);
  }
  const [mining, setMining] = useState<boolean>();
  const completeAction = useMountedCallback(onActionComplete);
  const key = items
    .map((item) => String(item.ref!.id))
    .sort()
    .join(",");
  const currentResult = result?.key === key ? result : undefined;
  const options = currentResult?.options;
  const error = currentResult?.error;
  const start = !unavailable && (options?.canStart ?? false);
  const stop = !unavailable && (options?.canStop ?? false);
  const showDisabledActions = unavailable || !options || !!options.unavailableReason;
  useEffect(() => {
    if (!open || !key || unavailable) return;
    const abort = new AbortController();
    const ids = key.split(",").map(BigInt);
    const request =
      kind === "tank"
        ? containersClient.getTankMiningOptions({ tankIds: ids }, { signal: abort.signal })
        : containersClient.getModuleMiningOptions({ moduleIds: ids }, { signal: abort.signal });
    void request
      .then((next) => {
        if (!abort.signal.aborted) setResult({ key, options: next });
      })
      .catch((cause) => {
        if (!abort.signal.aborted) setResult({ key, error: getErrorMessage(cause) });
      });
    return () => abort.abort();
  }, [open, key, kind, unavailable]);
  if (!items.length) return null;
  const label = items.length === 1 ? items[0].name : `${items.length} selected ${kind}s`;
  const actionLabel = `${kind === "tank" ? "Tank" : "Module"} actions`;
  const beginMiningAction = (nextMining: boolean) => {
    setMining(nextMining);
    onActionStart?.();
  };
  return (
    <div
      ref={containerRef}
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") event.stopPropagation();
      }}
    >
      <RowActionsMenu
        ariaLabel={`Actions for ${label}`}
        triggerLabel={presentation === "bulk" || items.length > 1 ? actionLabel : undefined}
        triggerClassName={
          presentation === "bulk"
            ? "bg-grayscale-white-10! text-grayscale-white-90!"
            : presentation === "header"
              ? "h-8! w-8! rounded-full p-0! text-text-primary [&_svg]:text-text-primary"
              : undefined
        }
        triggerVariant={presentation !== "row" ? variants.secondary : undefined}
        triggerSuffixIcon={presentation === "bulk" ? <ChevronDown width={iconSizes.xSmall} /> : undefined}
        testIdPrefix={`${kind}-${key}-actions`}
        onOpenChange={(nextOpen) => {
          if (nextOpen) setResult(undefined);
          setOpen(nextOpen);
        }}
        actions={[
          {
            label: "Update worker names",
            icon: <MiningPools />,
            hidden: !canRename || (oneContainer && groups?.supported === false),
            disabled: unavailable || (oneContainer && !canEditHashboardPools(groups)),
            onClick: () => setWorkersOpen(true),
          },
          {
            label: "Edit pool",
            icon:
              !unavailable && oneContainer && isFetching && !canEditHashboardPools(groups) ? (
                <ProgressCircular indeterminate size={18} dataTestId="pool-readiness-spinner" />
              ) : (
                <MiningPools />
              ),
            hidden: !canEditPools || (oneContainer && groups?.supported === false),
            disabled: unavailable || (oneContainer && !canEditHashboardPools(groups)),
            onClick: () => setPoolsOpen(true),
          },
          {
            label: "Start mining",
            icon: <Play />,
            hidden: !start && !showDisabledActions,
            disabled: !start,
            onClick: () => beginMiningAction(true),
          },
          {
            label: "Stop mining",
            icon: <Power />,
            hidden: !stop && !showDisabledActions,
            disabled: !stop,
            onClick: () => beginMiningAction(false),
          },
          {
            label: "Blink LEDs",
            icon: <LEDIndicator />,
            hidden: !canIdentify,
            disabled: unavailable,
            onClick: () => setIdentify(true),
          },
          {
            label: "Stop blinking LEDs",
            icon: <LEDIndicator />,
            hidden: !canIdentify,
            disabled: unavailable,
            onClick: () => setIdentify(false),
          },
          {
            label: unavailable
              ? "Controls unavailable until module status refreshes."
              : error || options?.unavailableReason || `Checking ${kind} controls…`,
            hidden: kind === "module" || !showDisabledActions,
            disabled: true,
            onClick: () => undefined,
          },
        ]}
      />
      {workersOpen ? (
        <WorkerNameDialog
          key={key}
          moduleIds={items.map((item) => item.ref!.id)}
          onDismiss={() => setWorkersOpen(false)}
        />
      ) : null}
      {poolsOpen ? (
        <EditHashboardPoolDialog
          key={`${items[0].deviceIdentifier}:${key}`}
          deviceIdentifier={items[0].deviceIdentifier}
          moduleIds={items.map((i) => i.ref!.id)}
          onClose={() => setPoolsOpen(false)}
        />
      ) : null}
      {identify !== undefined ? (
        <BoardIdentifyDialog
          key={`${key}:${identify}`}
          moduleIds={items.map((item) => item.ref!.id)}
          blink={identify}
          onClose={() => {
            setIdentify(undefined);
            containerRef.current?.querySelector<HTMLButtonElement>("button")?.focus();
          }}
        />
      ) : null}
      {mining !== undefined ? (
        <MiningDialog
          key={`${key}:${mining}`}
          items={items}
          kind={kind}
          mining={mining}
          unavailable={unavailable}
          onRefresh={() => onRefresh?.()}
          onClose={() => {
            setMining(undefined);
            setResult(undefined);
            containerRef.current?.querySelector<HTMLButtonElement>("button")?.focus();
          }}
          onActionComplete={completeAction}
        />
      ) : null}
    </div>
  );
}
