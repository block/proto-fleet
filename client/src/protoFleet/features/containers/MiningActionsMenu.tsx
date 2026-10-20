import { useEffect, useRef, useState } from "react";
import MiningDialog from "./MiningDialog";
import { containersClient } from "@/protoFleet/api/clients";
import type { GetModuleMiningOptionsResponse } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { ChevronDown, Play, Power } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
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
  const containerRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
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
            label: unavailable
              ? "Controls unavailable until module status refreshes."
              : error || options?.unavailableReason || `Checking ${kind} controls…`,
            hidden: !showDisabledActions,
            disabled: true,
            onClick: () => undefined,
          },
        ]}
      />
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
