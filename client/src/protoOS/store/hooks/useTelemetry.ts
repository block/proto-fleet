import { useMemo } from "react";

import { asAsicId, ROOT_MODULE_KEY } from "../contract";
import type { AsicTelemetry, FanTelemetry, HashboardTelemetry, PsuTelemetry } from "../contract";
import useMinerStore from "../useMinerStore";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";

export const useHashboardsTelemetry = (): HashboardTelemetry[] => {
  const values = useMinerStore((state) => state.telemetry.hashboards);
  return useMemo(() => [...values.values()], [values]);
};
export const useHashboardTelemetry = (serial: string): HashboardTelemetry | undefined =>
  useMinerStore((state) => state.telemetry.hashboards.get(getHashboardId(serial)));
export const useAsicsTelemetry = (): AsicTelemetry[] => {
  const values = useMinerStore((state) => state.telemetry.asics);
  return useMemo(() => [...values.values()], [values]);
};
export const useAsicTelemetry = (id: string): AsicTelemetry | undefined =>
  useMinerStore((state) => state.telemetry.asics.get(asAsicId(id)));
export const usePsusTelemetry = (): PsuTelemetry[] => {
  const values = useMinerStore((state) => state.telemetry.psus);
  return useMemo(() => [...values.values()], [values]);
};
export const usePsuTelemetry = (slot: number): PsuTelemetry | undefined =>
  useMinerStore((state) => state.telemetry.psus.get(getPsuId(slot)));
export const useFansTelemetry = (): FanTelemetry[] => {
  const values = useMinerStore((state) => state.telemetry.fans);
  return useMemo(() => [...values.values()], [values]);
};
export const useFanTelemetry = (slot: number): FanTelemetry | undefined =>
  useMinerStore((state) => state.telemetry.fans.get(getFanId(slot)));
export const useCoolingMode = () => useMinerStore((state) => state.telemetry.coolingModeByOwner.get(ROOT_MODULE_KEY));
