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

export default function MiningActionsMenu({
  items,
  kind,
  onRefresh,
  presentation = "row",
  onActionStart,
  onActionComplete,
}: {
  items: InventoryItem[];
  kind: "tank" | "module";
  onRefresh?: () => void;
  presentation?: "row" | "bulk";
  onActionStart?: () => void;
  onActionComplete?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [result, setResult] = useState<{
    key: string;
    options?: Pick<GetModuleMiningOptionsResponse, "canStart" | "canStop" | "unavailableReason">;
    error?: string;
  }>();
  const [mining, setMining] = useState<boolean>();
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const key = items
    .map((item) => String(item.ref!.id))
    .sort()
    .join(",");
  const currentResult = result?.key === key ? result : undefined;
  const options = currentResult?.options;
  const error = currentResult?.error;
  const start = options?.canStart ?? false;
  const stop = options?.canStop ?? false;
  const showDisabledActions = !options || !!options.unavailableReason;
  useEffect(() => {
    if (!open || !key) return;
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
  }, [open, key, kind]);
  if (!items.length) return null;
  const label = items.length === 1 ? items[0].name : `${items.length} selected ${kind}s`;
  const actionLabel = `${kind === "tank" ? "Tank" : "Module"} actions`;
  const beginMiningAction = (nextMining: boolean) => {
    setMining(nextMining);
    onActionStart?.();
  };
  return (
    <div
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") event.stopPropagation();
      }}
    >
      <RowActionsMenu
        ariaLabel={`Actions for ${label}`}
        triggerLabel={presentation === "bulk" || items.length > 1 ? actionLabel : undefined}
        triggerClassName={presentation === "bulk" ? "bg-grayscale-white-10! text-grayscale-white-90!" : undefined}
        triggerVariant={presentation === "bulk" ? variants.secondary : undefined}
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
            label: error || options?.unavailableReason || `Checking ${kind} controls…`,
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
          onRefresh={() => onRefresh?.()}
          onClose={() => {
            setMining(undefined);
            setResult(undefined);
          }}
          onActionComplete={() => {
            if (mounted.current) onActionComplete?.();
          }}
        />
      ) : null}
    </div>
  );
}
