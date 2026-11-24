import { useCallback, useEffect, useRef, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { awaitEquipmentCommand } from "../awaitEquipmentCommand";
import {
  alarmActionEnum,
  type AlarmActionName,
  alarmActionSupport,
  type AlarmOutcome,
  readbackOutcome,
} from "../plantAlarms";
import { containersClient } from "@/protoFleet/api/clients";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

const WINDOW_MS = 120_000;
const verb: Record<AlarmActionName, string> = { mute: "Mute", reset: "Reset" };
// The controller or Fleet declined before anything was written. Any other
// failure may have reached the controller, so its outcome is unknown.
const REFUSAL_CODES = new Set([
  Code.InvalidArgument,
  Code.FailedPrecondition,
  Code.Unimplemented,
  Code.PermissionDenied,
  Code.Unauthenticated,
  Code.NotFound,
]);

interface Pending {
  identifier: string;
  action: AlarmActionName;
  phase: "sending" | "pending" | "readback";
  batch?: string;
  /** Plant observation shown when the command was sent; only a newer one can judge it. */
  baseline?: number;
  after?: number;
  deadline: number;
}

export interface AlarmNotice {
  identifier: string;
  action: AlarmActionName;
  text: string;
  tone: AlarmOutcome["tone"] | "error";
  /** Unconfirmed results stay open to a later observation; this is where that comparison starts. */
  after?: number;
}

/**
 * Mute or reset a plant alarm. The command is submitted once and observed
 * without ever being resubmitted. Completion only acknowledges the write: the
 * outcome is judged from a plant observation newer than both the server-recorded
 * completion, as equipment controls do, and the observation shown at send time.
 */
export function usePlantAlarmAction(identifier: string, container: Container | undefined, refresh: () => void) {
  const [pending, setPending] = useState<Pending>();
  const [notice, setNotice] = useState<AlarmNotice>();
  const active = useRef<Pending | undefined>(undefined);
  const [routeIdentifier, setRouteIdentifier] = useState(identifier);
  if (routeIdentifier !== identifier) {
    setRouteIdentifier(identifier);
    setPending(undefined);
    setNotice(undefined);
  }
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;

  useEffect(() => {
    active.current = undefined;
    return () => {
      active.current = undefined;
    };
  }, [identifier]);

  const finish = useCallback(
    (operation: Pending, result: Pick<AlarmNotice, "text" | "tone"> & { unconfirmed?: boolean }) => {
      if (active.current !== operation) return;
      active.current = undefined;
      setPending(undefined);
      setNotice({
        identifier,
        action: operation.action,
        text: result.text,
        tone: result.tone,
        after: result.unconfirmed ? operation.after : undefined,
      });
      refreshRef.current();
    },
    [identifier],
  );

  useEffect(() => {
    if (!pending || pending.identifier !== identifier) return;
    if (pending.phase === "readback") {
      const timer = setTimeout(
        () =>
          finish(pending, {
            text: `${verb[pending.action]} sent, but no newer controller reading arrived to confirm it. Check the alarm state before trying again.`,
            tone: "warning",
            unconfirmed: true,
          }),
        Math.max(0, pending.deadline - Date.now()),
      );
      return () => clearTimeout(timer);
    }
    if (pending.phase !== "pending" || !pending.batch) return;
    const abort = new AbortController();
    void awaitEquipmentCommand(identifier, pending.batch, abort.signal, pending.deadline)
      .then((result) => {
        if (abort.signal.aborted || active.current !== pending) return;
        const completedAt = result.deviceResults[0]?.updatedAt;
        const next: Pending = {
          ...pending,
          phase: "readback",
          after: completedAt
            ? Math.max(timestampDate(completedAt).getTime(), pending.baseline ?? Number.NEGATIVE_INFINITY)
            : undefined,
          deadline: Date.now() + WINDOW_MS,
        };
        active.current = next;
        setPending(next);
        refreshRef.current();
      })
      .catch((error: unknown) => {
        if (!abort.signal.aborted) {
          finish(pending, {
            text:
              Date.now() >= pending.deadline
                ? "Command outcome unknown. Check the alarm state before trying again."
                : getErrorMessage(error, "Command failed; the result is unconfirmed."),
            tone: "error",
          });
        }
      });
    return () => abort.abort();
  }, [pending, identifier, finish]);

  useEffect(() => {
    const operation = active.current;
    if (!operation || operation.identifier !== identifier || operation.phase !== "readback") return;
    const outcome = readbackOutcome(operation.action, container?.plant, operation.after);
    if (outcome) finish(operation, outcome);
  }, [container, identifier, finish, pending]);

  const submit = async (action: AlarmActionName) => {
    if (active.current || !container?.canControlEquipment || alarmActionSupport(container, action) !== "supported") {
      return;
    }
    const observed = container.plant?.observedAt;
    const operation: Pending = {
      identifier,
      action,
      phase: "sending",
      baseline: observed ? timestampDate(observed).getTime() : undefined,
      deadline: Date.now() + WINDOW_MS,
    };
    active.current = operation;
    setPending(operation);
    setNotice(undefined);
    try {
      const result = await containersClient.runPlantAlarmAction(
        { deviceIdentifier: identifier, action: alarmActionEnum[action] },
        { timeoutMs: 10_000 },
      );
      if (active.current !== operation) return;
      if (!result.batchIdentifier) throw new Error("No command result was returned; the outcome is unconfirmed.");
      const next: Pending = { ...operation, phase: "pending", batch: result.batchIdentifier };
      active.current = next;
      setPending(next);
    } catch (error) {
      const message = getErrorMessage(error, "The request failed.");
      const refused = REFUSAL_CODES.has(ConnectError.from(error).code);
      finish(operation, {
        text: refused
          ? `${verb[action]} not sent. ${message}`
          : `${verb[action]} outcome unknown: ${message.replace(/\.$/, "")}. Check the alarm state before trying again.`,
        tone: "error",
      });
    }
  };

  const current = pending?.identifier === identifier ? pending : undefined;
  const shown = notice?.identifier === identifier ? notice : undefined;
  // An unconfirmed result is settled by a later observation instead of waiting for a manual check.
  const settled = shown?.after !== undefined ? readbackOutcome(shown.action, container?.plant, shown.after) : undefined;
  return {
    submit,
    busy: !!current,
    status: current
      ? current.phase === "sending"
        ? `Sending ${current.action}…`
        : current.phase === "pending"
          ? "Waiting for the controller to accept the command…"
          : "Waiting for the controller to report the result…"
      : undefined,
    notice: settled && shown ? { ...shown, ...settled, after: undefined } : shown,
    dismiss: () => setNotice(undefined),
  };
}
