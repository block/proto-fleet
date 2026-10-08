import { useEffect, useRef, useState } from "react";
import TankMiningDialog from "./TankMiningDialog";
import { containersClient } from "@/protoFleet/api/clients";
import type { GetTankMiningOptionsResponse } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { ChevronDown, Play, Power } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import RowActionsMenu from "@/shared/components/RowActionsMenu";

export default function TankActionsMenu({
  tanks,
  onRefresh,
  presentation = "row",
  onActionStart,
  onActionComplete,
}: {
  tanks: InventoryItem[];
  onRefresh: () => void;
  presentation?: "row" | "bulk";
  onActionStart?: () => void;
  onActionComplete?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [result, setResult] = useState<{ key: string; options?: GetTankMiningOptionsResponse; error?: string }>();
  const [mining, setMining] = useState<boolean>();
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const key = tanks
    .map((tank) => String(tank.ref!.id))
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
    void containersClient
      .getTankMiningOptions({ tankIds: key.split(",").map(BigInt) }, { signal: abort.signal })
      .then((next) => {
        if (!abort.signal.aborted) setResult({ key, options: next });
      })
      .catch((cause) => {
        if (!abort.signal.aborted) setResult({ key, error: getErrorMessage(cause) });
      });
    return () => abort.abort();
  }, [open, key]);
  if (!tanks.length) return null;
  const label = tanks.length === 1 ? tanks[0].name : `${tanks.length} selected tanks`;
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
        triggerLabel={presentation === "bulk" || tanks.length > 1 ? "Tank actions" : undefined}
        triggerClassName={presentation === "bulk" ? "bg-grayscale-white-10! text-grayscale-white-90!" : undefined}
        triggerVariant={presentation === "bulk" ? variants.secondary : undefined}
        triggerSuffixIcon={presentation === "bulk" ? <ChevronDown width={iconSizes.xSmall} /> : undefined}
        testIdPrefix={`tank-${key}-actions`}
        onOpenChange={(nextOpen) => {
          if (nextOpen) setResult(undefined);
          setOpen(nextOpen);
        }}
        actions={[
          ...(start || showDisabledActions
            ? [
                {
                  label: "Start mining",
                  icon: <Play />,
                  disabled: !start,
                  onClick: () => beginMiningAction(true),
                },
              ]
            : []),
          ...(stop || showDisabledActions
            ? [
                {
                  label: "Stop mining",
                  icon: <Power />,
                  disabled: !stop,
                  onClick: () => beginMiningAction(false),
                },
              ]
            : []),
          ...(showDisabledActions
            ? [
                {
                  label: error || options?.unavailableReason || "Checking tank controls…",
                  disabled: true,
                  onClick: () => undefined,
                },
              ]
            : []),
        ]}
      />
      {mining !== undefined ? (
        <TankMiningDialog
          key={`${key}:${mining}`}
          tanks={tanks}
          mining={mining}
          onRefresh={() => {
            if (mounted.current) onRefresh();
          }}
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
