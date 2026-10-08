import { useEffect, useRef, useState } from "react";
import { containersClient } from "@/protoFleet/api/clients";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { Play, Power } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

export default function TankMiningDialog({
  tanks,
  mining,
  onClose,
  onRefresh,
}: {
  tanks: InventoryItem[];
  mining: boolean;
  onClose: () => void;
  onRefresh: () => void;
}) {
  const [phase, setPhase] = useState<"confirm" | "pending" | "done">("confirm");
  const busy = phase === "pending";
  const [error, setError] = useState<string>();
  const [results, setResults] = useState<{ label: string; text: string; failed: boolean }[]>([]);
  const submitted = useRef(false);
  const abort = useRef(new AbortController());
  useEffect(() => {
    const controller = new AbortController();
    abort.current = controller;
    return () => controller.abort();
  }, []);
  const action = mining ? "Start mining" : "Stop mining";
  const target = tanks.length === 1 ? tanks[0].name : `the ${tanks.length} selected tanks`;
  const pendingMessage = `${mining ? "Starting" : "Stopping"} mining…`;
  const submit = async () => {
    if (submitted.current) return;
    submitted.current = true;
    setPhase("pending");
    const signal = abort.current.signal;
    try {
      const { commands } = await containersClient.setTankMining(
        { tankIds: tanks.map((tank) => tank.ref!.id), mining },
        { signal, timeoutMs: 10_000 },
      );
      if (!commands.length) throw new Error("Couldn't confirm whether the request was sent.");
      const outcomes = await Promise.all(
        commands.map(async (command) => {
          const names = tanks
            .filter((tank) => command.tankIds.includes(tank.ref!.id))
            .map((tank) => tank.name)
            .join(", ");
          const label = `${command.containerName}: ${names}`;
          const deadline = Date.now() + 120_000;
          try {
            while (Date.now() < deadline) {
              const { result } = await containersClient.getEquipmentCommandResult(
                { deviceIdentifier: command.deviceIdentifier, batchIdentifier: command.batchIdentifier },
                { signal, timeoutMs: 10_000 },
              );
              if (result?.status === "finished") {
                const failed = result.failureCount > 0 || result.successCount !== 1 || result.detailsPruned;
                return {
                  label,
                  failed,
                  text: failed
                    ? result.deviceResults.find((device) => device.errorMessage)?.errorMessage ||
                      "Couldn't confirm the mining state. Check the readings before trying again."
                    : `${mining ? "Start" : "Stop"} request accepted. Check the readings to confirm mining has ${mining ? "started" : "stopped"}.`,
                };
              }
              await new Promise<void>((resolve, reject) => {
                const timer = setTimeout(() => {
                  signal.removeEventListener("abort", cancel);
                  resolve();
                }, 1000);
                const cancel = () => {
                  clearTimeout(timer);
                  reject(new Error("Couldn't confirm the result."));
                };
                signal.addEventListener("abort", cancel, { once: true });
                if (signal.aborted) cancel();
              });
            }
            throw new Error("Couldn't confirm the result. Refresh the readings before trying again.");
          } catch (cause) {
            return { label, failed: true, text: getErrorMessage(cause) };
          }
        }),
      );
      if (!signal.aborted) setResults(outcomes);
    } catch (cause) {
      if (!signal.aborted) setError(`${getErrorMessage(cause)} Refresh the readings before trying again.`);
    } finally {
      if (!signal.aborted) {
        setPhase("done");
        onRefresh();
      }
    }
  };
  return (
    <Dialog
      open
      title={phase === "confirm" ? `${action}${tanks.length > 1 ? ` on ${tanks.length} tanks` : ""}?` : action}
      subtitle={
        phase === "confirm"
          ? `All modules in ${target} will ${mining ? "start" : "stop"} hashing.`
          : tanks.length === 1
            ? tanks[0].name
            : `${tanks.length} selected tanks`
      }
      subtitleSize="text-300"
      icon={<DialogIcon intent={mining ? "success" : "info"}>{mining ? <Play /> : <Power />}</DialogIcon>}
      loading={busy}
      testId="tank-mining-dialog"
      onDismiss={busy ? undefined : onClose}
      buttons={
        phase !== "confirm"
          ? [
              {
                text: busy ? pendingMessage : "Done",
                variant: variants.secondary,
                onClick: onClose,
                disabled: busy,
              },
            ]
          : [
              { text: "Cancel", variant: variants.secondary, onClick: onClose },
              { text: action, variant: variants.primary, onClick: () => void submit() },
            ]
      }
    >
      {tanks.length > 1 ? (
        <ul className="flex flex-col gap-2">
          {tanks.map((tank) => (
            <li key={String(tank.ref!.id)}>
              {tank.facilityName && !tank.name.startsWith(tank.facilityName) ? `${tank.facilityName}: ` : ""}
              {tank.name}
            </li>
          ))}
        </ul>
      ) : null}
      {busy ? (
        <p role="status" className="mt-4">
          {pendingMessage}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-core-negative mt-4">
          {error}
        </p>
      ) : null}
      {results.map((result) => (
        <p key={result.label} role={result.failed ? "alert" : "status"} className="mt-4">
          {tanks.length > 1 ? `${result.label}: ` : ""}
          {result.text}
        </p>
      ))}
    </Dialog>
  );
}
