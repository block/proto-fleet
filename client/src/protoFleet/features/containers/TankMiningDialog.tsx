import { useEffect, useRef, useState } from "react";
import { containersClient } from "@/protoFleet/api/clients";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

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
      if (!commands.length) throw new Error("No command result returned; outcome unconfirmed.");
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
                      "Command failed; state unconfirmed."
                    : `${action} acknowledged. Current readings will refresh.`,
                };
              }
              await new Promise<void>((resolve, reject) => {
                const timer = setTimeout(() => {
                  signal.removeEventListener("abort", cancel);
                  resolve();
                }, 1000);
                const cancel = () => {
                  clearTimeout(timer);
                  reject(new Error("Command outcome unconfirmed."));
                };
                signal.addEventListener("abort", cancel, { once: true });
                if (signal.aborted) cancel();
              });
            }
            throw new Error("Command outcome unconfirmed. Refresh before retrying.");
          } catch (cause) {
            return { label, failed: true, text: getErrorMessage(cause) };
          }
        }),
      );
      if (!signal.aborted) setResults(outcomes);
    } catch (cause) {
      if (!signal.aborted) setError(`${getErrorMessage(cause)} Refresh before retrying.`);
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
      title={`${action}: ${tanks.length === 1 ? tanks[0].name : `${tanks.length} tanks`}`}
      subtitle="Applies to all modules in the selected tanks. The controller reports any boards it cannot update."
      testId="tank-mining-dialog"
      onDismiss={busy ? undefined : onClose}
      buttons={
        phase !== "confirm"
          ? [
              {
                text: busy ? "Command pending…" : "Done",
                variant: variants.secondary,
                onClick: onClose,
                disabled: busy,
              },
            ]
          : [
              { text: "Cancel", variant: variants.secondary, onClick: onClose },
              { text: "Confirm", variant: variants.primary, onClick: () => void submit() },
            ]
      }
    >
      {tanks.length > 1 ? (
        <ul className="flex flex-col gap-2">
          {tanks.map((tank) => (
            <li key={String(tank.ref!.id)}>
              {tank.facilityName ? `${tank.facilityName}: ` : ""}
              {tank.name}
            </li>
          ))}
        </ul>
      ) : null}
      {busy ? (
        <p role="status" className="mt-4">
          {action} pending for the selected tanks…
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-core-negative mt-4">
          {error}
        </p>
      ) : null}
      {results.map((result) => (
        <p key={result.label} role={result.failed ? "alert" : "status"} className="mt-4">
          {result.label}: {result.text}
        </p>
      ))}
    </Dialog>
  );
}
