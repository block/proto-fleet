import { useMemo } from "react";

import { asAsicId, ROOT_MODULE_KEY } from "../contract";
import {
  asicTelemetryView,
  fanTelemetryView,
  hashboardTelemetryView,
  minerTelemetryView,
  psuTelemetryView,
} from "../selectors/legacyTelemetry";
import useMinerStore from "../useMinerStore";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";

export const useMinerTelemetry = () => {
  const rollup = useMinerStore((state) => state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY));
  const hashboards = useMinerStore((state) => state.hardware.hashboards);
  return useMemo(
    () =>
      minerTelemetryView(
        rollup,
        [...hashboards.values()].map((board) => board.identity.value),
      ),
    [hashboards, rollup],
  );
};
export const useHashboardsTelemetry = () => {
  const values = useMinerStore((state) => state.telemetry.hashboards);
  return useMemo(() => [...values.values()].map(hashboardTelemetryView), [values]);
};
export const useHashboardTelemetry = (serial: string) => {
  const value = useMinerStore((state) => state.telemetry.hashboards.get(getHashboardId(serial)));
  return useMemo(() => (value ? hashboardTelemetryView(value) : undefined), [value]);
};
export const useAsicsTelemetry = () => {
  const values = useMinerStore((state) => state.telemetry.asics);
  return useMemo(() => [...values.values()].map(asicTelemetryView), [values]);
};
export const useAsicTelemetry = (id: string) => {
  const value = useMinerStore((state) => state.telemetry.asics.get(asAsicId(id)));
  return useMemo(() => (value ? asicTelemetryView(value) : undefined), [value]);
};
export const usePsusTelemetry = () => {
  const values = useMinerStore((state) => state.telemetry.psus);
  return useMemo(() => [...values.values()].map(psuTelemetryView), [values]);
};
export const usePsuTelemetry = (slot: number) => {
  const value = useMinerStore((state) => state.telemetry.psus.get(getPsuId(slot)));
  return useMemo(() => (value ? psuTelemetryView(value) : undefined), [value]);
};
export const useFansTelemetry = () => {
  const values = useMinerStore((state) => state.telemetry.fans);
  return useMemo(() => [...values.values()].map(fanTelemetryView), [values]);
};
export const useFanTelemetry = (slot: number) => {
  const value = useMinerStore((state) => state.telemetry.fans.get(getFanId(slot)));
  return useMemo(() => (value ? fanTelemetryView(value) : undefined), [value]);
};
export const useCoolingMode = () => useMinerStore((state) => state.telemetry.coolingModeByOwner.get(ROOT_MODULE_KEY));
export const useIntervalMs = () =>
  useMinerStore(
    (state) => state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.bucketSizeMs ?? 900_000,
  );
