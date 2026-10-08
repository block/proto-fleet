import { useEffect, useState } from "react";
import TankMiningDialog from "./TankMiningDialog";
import { containersClient } from "@/protoFleet/api/clients";
import type { GetTankMiningOptionsResponse } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { Play, Power } from "@/shared/assets/icons";
import RowActionsMenu from "@/shared/components/RowActionsMenu";

export default function TankActionsMenu({ tanks, onRefresh }: { tanks: InventoryItem[]; onRefresh: () => void }) {
  const [open, setOpen] = useState(false);
  const [result, setResult] = useState<{ key: string; options?: GetTankMiningOptionsResponse; error?: string }>();
  const [mining, setMining] = useState<boolean>();
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
  return (
    <div
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") event.stopPropagation();
      }}
    >
      <RowActionsMenu
        ariaLabel={`Actions for ${label}`}
        triggerLabel={tanks.length > 1 ? "Tank actions" : undefined}
        testIdPrefix={`tank-${key}-actions`}
        onOpenChange={(nextOpen) => {
          if (nextOpen) setResult(undefined);
          setOpen(nextOpen);
        }}
        actions={[
          ...(start || showDisabledActions
            ? [{ label: "Start mining", icon: <Play />, disabled: !start, onClick: () => setMining(true) }]
            : []),
          ...(stop || showDisabledActions
            ? [{ label: "Stop mining", icon: <Power />, disabled: !stop, onClick: () => setMining(false) }]
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
          onRefresh={onRefresh}
          onClose={() => {
            setMining(undefined);
            setResult(undefined);
          }}
        />
      ) : null}
    </div>
  );
}
