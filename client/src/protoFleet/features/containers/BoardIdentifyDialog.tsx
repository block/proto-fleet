import { useEffect, useRef, useState } from "react";
import { timestampMs } from "@bufbuild/protobuf/wkt";
import { awaitEquipmentCommand } from "./awaitEquipmentCommand";
import { containersClient } from "@/protoFleet/api/clients";
import {
  BoardIdentifyMode,
  type GetBoardIdentifyOptionsResponse,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

type Outcome =
  { status: "pending" } | { status: "confirmed"; blinkingUntil?: number } | { status: "failed"; message: string };

// Frigate owns the blink window (15 minutes) and MDK's expires_at is not
// carried through Fleet command results, so the end time is approximate:
// counted from when the controller's result was recorded.
const BLINK_MS = 15 * 60_000;
const clock = (ms: number) => new Date(ms).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });

export default function BoardIdentifyDialog({
  moduleIds = [],
  hashboardIds = [],
  blink,
  onClose,
}: {
  moduleIds?: bigint[];
  hashboardIds?: bigint[];
  blink: boolean;
  onClose: () => void;
}) {
  const [options, setOptions] = useState<GetBoardIdentifyOptionsResponse>();
  const [error, setError] = useState<string>();
  const [sending, setSending] = useState(false);
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>();
  // Bumped after a rejected submit so the operator reviews current boards before retrying.
  const [refresh, setRefresh] = useState(0);
  const submitted = useRef(false);
  const abort = useRef(new AbortController());
  const selection = `${moduleIds.join(",")}|${hashboardIds.join(",")}`;
  const action = blink ? "Blink LEDs" : "Stop blinking LEDs";

  useEffect(() => {
    const controller = new AbortController();
    const [modules, boards] = selection.split("|").map((ids) => (ids ? ids.split(",").map(BigInt) : []));
    void containersClient
      .getBoardIdentifyOptions({ moduleIds: modules, hashboardIds: boards }, { signal: controller.signal })
      .then((next) => {
        if (!controller.signal.aborted) setOptions(next);
      })
      .catch((cause) => {
        if (!controller.signal.aborted) setError(getErrorMessage(cause));
      });
    return () => controller.abort();
  }, [selection, refresh]);
  useEffect(() => {
    const pending = abort.current;
    return () => pending.abort();
  }, []);

  const boards = options?.boards ?? [];
  const submit = async () => {
    if (submitted.current || !options?.canIdentify) return;
    submitted.current = true;
    setSending(true);
    setError(undefined);
    try {
      // Dispatch exactly the installations the operator confirmed; the server
      // rejects the request if any board has since moved or been replaced.
      const { commands } = await containersClient.setBoardIdentify(
        {
          boards: boards.map((board) => ({ hashboardId: board.hashboardId, installationId: board.installationId })),
          mode: blink ? BoardIdentifyMode.BLINK : BoardIdentifyMode.OFF,
        },
        { timeoutMs: 10_000 },
      );
      setOutcomes(Object.fromEntries(commands.map((command) => [String(command.hashboardId), { status: "pending" }])));
      await Promise.all(
        commands.map(async (command) => {
          let outcome: Outcome;
          try {
            const result = await awaitEquipmentCommand(
              command.deviceIdentifier,
              command.batchIdentifier,
              abort.current.signal,
            );
            const recordedAt = result.deviceResults[0]?.updatedAt;
            outcome = {
              status: "confirmed",
              blinkingUntil: blink ? (recordedAt ? timestampMs(recordedAt) : Date.now()) + BLINK_MS : undefined,
            };
          } catch (cause) {
            outcome = { status: "failed", message: getErrorMessage(cause) };
          }
          if (!abort.current.signal.aborted) {
            setOutcomes((current) => ({ ...current, [String(command.hashboardId)]: outcome }));
          }
        }),
      );
    } catch (cause) {
      submitted.current = false;
      if (!abort.current.signal.aborted) {
        setError(getErrorMessage(cause));
        setOptions(undefined);
        setRefresh((count) => count + 1);
      }
    } finally {
      if (!abort.current.signal.aborted) setSending(false);
    }
  };

  const describe = (outcome: Outcome | undefined) => {
    if (!outcome) return undefined;
    if (outcome.status === "pending") return "Waiting for controller…";
    if (outcome.status === "confirmed") {
      return outcome.blinkingUntil === undefined ? "Stopped" : `Blinking until about ${clock(outcome.blinkingUntil)}`;
    }
    return outcome.message;
  };
  const unavailable = options && !options.canIdentify ? options.unavailableReason : undefined;
  const loading = options === undefined && error === undefined;
  return (
    <Dialog
      open
      loading={loading}
      title={options ? `${action} on ${boards.length} ${boards.length === 1 ? "board" : "boards"}?` : `${action}?`}
      subtitle={
        blink
          ? "The identification LED on each listed board will blink for about 15 minutes."
          : "The identification LED on each listed board will stop blinking."
      }
      // Escape must not hide the dialog while the request is in flight: the
      // commands may still queue, and the operator would miss their outcomes.
      onDismiss={sending && !outcomes ? undefined : onClose}
      testId="board-identify-dialog"
      manageFocus
      buttons={
        outcomes
          ? [{ text: "Close", variant: variants.primary, onClick: onClose, disabled: false }]
          : [
              { text: "Cancel", variant: variants.secondary, onClick: onClose, disabled: sending },
              {
                text: sending ? "Sending…" : action,
                variant: variants.primary,
                onClick: () => void submit(),
                disabled: sending || !options?.canIdentify,
              },
            ]
      }
    >
      {boards.length ? (
        <ul aria-label="Affected boards" className="flex flex-col gap-2">
          {boards.map((board) => {
            const outcome = outcomes?.[String(board.hashboardId)];
            return (
              <li key={String(board.hashboardId)} className="flex items-baseline justify-between gap-4">
                <span>
                  {board.containerName} · Tank {board.tank} · Module {board.module} · Board {board.slot}
                  {board.serialNumber ? (
                    <span className="block text-200 text-text-primary-70">{board.serialNumber}</span>
                  ) : null}
                </span>
                {outcome ? (
                  <span
                    role="status"
                    className={
                      outcome.status === "failed"
                        ? "text-right text-200 text-intent-critical-text"
                        : "text-right text-200 text-text-primary-70"
                    }
                  >
                    {describe(outcome)}
                  </span>
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : null}
      {unavailable || error ? (
        <p role="alert" className="mt-3 text-intent-critical-text">
          {error ?? unavailable}
        </p>
      ) : null}
    </Dialog>
  );
}
