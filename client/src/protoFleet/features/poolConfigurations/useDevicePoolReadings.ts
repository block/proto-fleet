import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Client } from "@connectrpc/connect";
import { CheckConfigurationResponseSchema } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import type {
  CheckConfigurationResponse,
  EquipmentAssignment,
  PoolConfigurationService,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useFleetStore } from "@/protoFleet/store";

export const poolTargetKey = (a: EquipmentAssignment) =>
  `${a.installationId ? "hashboard_installation" : "device"}:${a.installationId || a.deviceId}`;

// Clears have no configuration ID, but their tracked outcome still owns status.
export const usesDevicePoolReadback = (a: Pick<EquipmentAssignment, "configurationId" | "operationId">) =>
  !a.configurationId && !a.operationId;

// Batch equipment without Fleet intent so the server can share one observation
// per controller. Assigned configurations retain their explicit verification flow.
export const useDevicePoolReadings = (
  client: Client<typeof PoolConfigurationService>,
  accountId: string,
  assignments: EquipmentAssignment[],
  enabled: boolean,
) => {
  // Include controller routing so moving a board invalidates an in-flight read.
  const selection = [
    ...new Set(assignments.filter(usesDevicePoolReadback).map((a) => `${a.deviceId}/${poolTargetKey(a)}`)),
  ]
    .sort()
    .join(",");
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const [revision, setRevision] = useState(0);
  const inFlight = useRef<AbortController | null>(null);
  const activeSelection = enabled ? selection : "";
  const [result, setResult] = useState(() => ({
    client,
    accountId,
    generation,
    selection: activeSelection,
    readings: {} as Record<string, CheckConfigurationResponse>,
  }));
  const sameScope = result.accountId === accountId && result.client === client && result.generation === generation;
  if (!sameScope || result.selection !== activeSelection) {
    // Prune before children render. Targets that leave and later return must
    // get fresh evidence, while unchanged targets can keep their last reading.
    const readings: Record<string, CheckConfigurationResponse> = {};
    if (sameScope) {
      for (const target of activeSelection.split(",")) {
        if (result.readings[target]) readings[target] = result.readings[target];
      }
    }
    setResult({ client, accountId, generation, selection: activeSelection, readings });
  }
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const poll = useCallback(() => {
    if (!inFlight.current) refresh();
  }, [refresh]);
  useEffect(() => {
    if (!enabled || !selection) return;
    const selectedTargets = selection.split(",");
    const publish = (batch: Record<string, CheckConfigurationResponse>) => {
      setResult((previous) => {
        // Keep only the same physical targets within the initiating account,
        // session and client. A selection change must not blank unrelated rows.
        const retained =
          previous?.accountId === accountId && previous.client === client && previous.generation === generation
            ? previous.readings
            : {};
        const readings: Record<string, CheckConfigurationResponse> = {};
        for (const target of selectedTargets) {
          const reading = batch[target] ?? retained[target];
          if (reading) readings[target] = reading;
        }
        return { client, accountId, generation, selection, readings };
      });
    };
    const controller = new AbortController();
    inFlight.current = controller;
    const settle = () => {
      if (inFlight.current === controller) inFlight.current = null;
    };
    const batches: string[][] = [];
    let keys: string[] = [];
    let devices = new Set<string>();
    for (const selected of selectedTargets) {
      const [deviceId, key] = selected.split("/");
      // Missing routing must not let unrelated boards count as one controller.
      const device = deviceId === "0" ? key : deviceId;
      if (keys.length === 500 || (!devices.has(device) && devices.size === 4)) {
        batches.push(keys);
        keys = [];
        devices = new Set();
      }
      keys.push(selected);
      devices.add(device);
    }
    if (keys.length) batches.push(keys);
    const unavailable = () =>
      create(CheckConfigurationResponseSchema, {
        state: "unavailable",
        resultCode: "check_unavailable",
      });
    const read = async () => {
      // Each request fits one server wave (four controllers, 500 targets).
      // Continue after failures so slow controllers cannot starve later ones.
      for (const keys of batches) {
        if (controller.signal.aborted) return;
        const routes = new Map(keys.map((selected) => [selected.split("/")[1], selected]));
        const requestTargets = [...routes.keys()].map((key) => {
          const [kind, id] = key.split(":");
          return { kind, targetId: BigInt(id) };
        });
        const batch: Record<string, CheckConfigurationResponse> = {};
        for (const key of keys) batch[key] = unavailable();
        try {
          const response = await client.readDevicePools(
            { accountId, targets: requestTargets },
            { signal: controller.signal, timeoutMs: 35_000 },
          );
          for (const { target, reading } of response.readings) {
            if (!target || !reading) continue;
            const key = `${target.kind}:${target.targetId}`;
            const route = routes.get(key);
            if (route) batch[route] = reading;
          }
        } catch {
          // Failed or missing results replace previous evidence with unavailable;
          // they must never make an old pool stack appear current or empty.
        }
        if (controller.signal.aborted) return;
        // Each settled batch replaces its own evidence, including real errors.
        // Later batches retain their last reading until their refresh settles.
        publish(batch);
      }
    };
    void read().finally(settle);
    return () => {
      settle();
      controller.abort();
    };
  }, [client, accountId, selection, enabled, revision, generation]);
  const readings = useMemo(() => {
    const current: Record<string, CheckConfigurationResponse> = {};
    if (enabled && result?.accountId === accountId && result.client === client && result.generation === generation) {
      for (const selected of selection.split(",")) {
        if (result.readings[selected]) current[selected.split("/")[1]] = result.readings[selected];
      }
    }
    return current;
  }, [enabled, result, accountId, client, generation, selection]);
  return {
    readings,
    refresh,
    poll,
  };
};
