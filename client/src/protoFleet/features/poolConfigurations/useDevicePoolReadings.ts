import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Client } from "@connectrpc/connect";
import { CheckConfigurationResponseSchema } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import type {
  CheckConfigurationResponse,
  EquipmentAssignment,
  PoolConfigurationService,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

export const poolTargetKey = (a: EquipmentAssignment) =>
  `${a.installationId ? "hashboard_installation" : "device"}:${a.installationId || a.deviceId}`;

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
    ...new Set(assignments.filter((a) => !a.configurationId).map((a) => `${a.deviceId}/${poolTargetKey(a)}`)),
  ]
    .sort()
    .join(",");
  const [revision, setRevision] = useState(0);
  const inFlight = useRef<AbortController | null>(null);
  const [result, setResult] = useState<{
    selection: string;
    accountId: string;
    readings: Record<string, CheckConfigurationResponse>;
  }>();
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const poll = useCallback(() => {
    if (!inFlight.current) refresh();
  }, [refresh]);
  useEffect(() => {
    if (!enabled || !selection) return;
    const controller = new AbortController();
    inFlight.current = controller;
    const settle = () => {
      if (inFlight.current === controller) inFlight.current = null;
    };
    const batches: string[][] = [];
    let keys: string[] = [];
    let devices = new Set<string>();
    for (const selected of selection.split(",")) {
      const [deviceId, key] = selected.split("/");
      // Missing routing must not let unrelated boards count as one controller.
      const device = deviceId === "0" ? key : deviceId;
      if (keys.length === 500 || (!devices.has(device) && devices.size === 4)) {
        batches.push(keys);
        keys = [];
        devices = new Set();
      }
      keys.push(key);
      devices.add(device);
    }
    if (keys.length) batches.push(keys);
    const readings: Record<string, CheckConfigurationResponse> = {};
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
        const requestTargets = keys.map((key) => {
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
            if (Object.prototype.hasOwnProperty.call(batch, key)) batch[key] = reading;
          }
        } catch {
          // Failed or missing results replace previous evidence with unavailable;
          // they must never make an old pool stack appear current or empty.
        }
        if (controller.signal.aborted) return;
        Object.assign(readings, batch);
        setResult({ selection, accountId, readings: { ...readings } });
      }
    };
    void read().finally(settle);
    return () => {
      settle();
      controller.abort();
    };
  }, [client, accountId, selection, enabled, revision]);
  return {
    readings: enabled && result?.selection === selection && result.accountId === accountId ? result.readings : {},
    refresh,
    poll,
  };
};
