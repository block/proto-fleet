import { useCallback, useEffect, useRef, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { awaitEquipmentCommand } from "../awaitEquipmentCommand";
import { containersClient } from "@/protoFleet/api/clients";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

interface Pending {
  identifier: string;
  name: string;
  enabled: boolean;
  phase: "sending" | "pending" | "readback";
  batch?: string;
  after?: number;
  deadline: number;
}

export function equipmentState(container: Container | undefined, name: string) {
  const c = container?.controls;
  if (
    !c ||
    c.stale ||
    !c.observedAt ||
    Date.now() - timestampDate(c.observedAt).getTime() > 120_000 ||
    timestampDate(c.observedAt).getTime() > Date.now() + 5000
  )
    return undefined;
  if (name === "main_circuit_breaker")
    return c.mainCircuitBreakerSupported === true ? c.mainCircuitBreakerClosed : undefined;
  return c.supported === true ? c.controls.find((control) => control.name === name)?.enabled : undefined;
}

/** Command completion acknowledges a write. Only a later equipment observation confirms it. */
export function useEquipmentCommand(identifier: string, container: Container | undefined, refresh: () => void) {
  const [pending, setPending] = useState<Pending>();
  const [notice, setNotice] = useState<{ identifier: string; text: string; error?: boolean }>();
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
    (operation: Pending, text: string, error = false) => {
      if (active.current !== operation) return;
      active.current = undefined;
      setPending(undefined);
      setNotice({ identifier, text, error });
      refreshRef.current();
    },
    [identifier],
  );

  useEffect(() => {
    if (!pending || pending.identifier !== identifier) return;
    if (pending.phase === "readback") {
      const timer = setTimeout(
        () => finish(pending, "Write acknowledged; physical state remains unconfirmed. Refresh before retrying.", true),
        Math.max(0, pending.deadline - Date.now()),
      );
      return () => clearTimeout(timer);
    }
    if (pending.phase !== "pending" || !pending.batch) return;
    const abort = new AbortController();
    void awaitEquipmentCommand(identifier, pending.batch, abort.signal, pending.deadline)
      .then((result) => {
        if (abort.signal.aborted || active.current !== pending) return;
        // Only telemetry newer than server-recorded command completion confirms state.
        const completedAt = result.deviceResults[0]?.updatedAt;
        const next: Pending = {
          ...pending,
          phase: "readback",
          after: completedAt ? timestampDate(completedAt).getTime() : undefined,
          deadline: Date.now() + 120_000,
        };
        active.current = next;
        setPending(next);
        refreshRef.current();
      })
      .catch((error: unknown) => {
        if (!abort.signal.aborted) {
          finish(
            pending,
            Date.now() >= pending.deadline
              ? "Command outcome unknown. Refresh before retrying."
              : getErrorMessage(error),
            true,
          );
        }
      });
    return () => abort.abort();
  }, [pending, identifier, finish]);

  useEffect(() => {
    const operation = active.current;
    if (!operation || operation.identifier !== identifier || operation.phase !== "readback") return;
    const observed = container?.controls?.observedAt;
    if (
      observed &&
      operation.after !== undefined &&
      timestampDate(observed).getTime() > operation.after &&
      equipmentState(container, operation.name) === operation.enabled
    ) {
      finish(operation, "Equipment state confirmed by the controller.");
    }
  }, [container, identifier, finish, pending]);

  const submit = async (name: string, enabled: boolean) => {
    if (active.current || equipmentState(container, name) === undefined) return;
    const operation: Pending = { identifier, name, enabled, phase: "sending", deadline: Date.now() + 120_000 };
    active.current = operation;
    setPending(operation);
    setNotice(undefined);
    try {
      const result = await containersClient.setEquipmentControl(
        { deviceIdentifier: identifier, name, enabled },
        { timeoutMs: 10_000 },
      );
      if (active.current !== operation) return;
      if (!result.batchIdentifier) throw new Error("No command result was returned; state unconfirmed.");
      const next: Pending = { ...operation, phase: "pending", batch: result.batchIdentifier };
      active.current = next;
      setPending(next);
    } catch (error) {
      finish(operation, getErrorMessage(error), true);
    }
  };
  const current = pending?.identifier === identifier ? pending : undefined;
  return {
    submit,
    busy: !!current,
    pendingName: current?.name,
    message: current
      ? current.phase === "readback"
        ? "Write acknowledged; waiting for controller readback…"
        : "Equipment command pending…"
      : notice?.identifier === identifier
        ? notice.text
        : undefined,
    failed: !current && notice?.identifier === identifier && notice.error,
  };
}
