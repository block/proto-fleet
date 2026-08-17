import { useShallow } from "zustand/react/shallow";

import { asAsicId, asComponentId, asHashboardId, ROOT_MODULE_KEY } from "../contract";
import {
  asicTelemetryView,
  fanTelemetryView,
  hashboardTelemetryView,
  minerTelemetryView,
  psuTelemetryView,
} from "../selectors/legacyTelemetry";
import useMinerStore from "../useMinerStore";

export const useMinerTelemetry = () =>
  useMinerStore((state) =>
    minerTelemetryView(
      state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY),
      [...state.hardware.hashboards.values()].map((board) => board.identity.value),
    ),
  );
export const useHashboardsTelemetry = () =>
  useMinerStore(useShallow((state) => [...state.telemetry.hashboards.values()].map(hashboardTelemetryView)));
export const useHashboardTelemetry = (serial: string) =>
  useMinerStore((state) => {
    const value = state.telemetry.hashboards.get(asHashboardId(`serial:${serial}`));
    return value ? hashboardTelemetryView(value) : undefined;
  });
export const useAsicsTelemetry = () =>
  useMinerStore(useShallow((state) => [...state.telemetry.asics.values()].map(asicTelemetryView)));
export const useAsicTelemetry = (id: string) =>
  useMinerStore((state) => {
    const value = state.telemetry.asics.get(asAsicId(id));
    return value ? asicTelemetryView(value) : undefined;
  });
export const usePsusTelemetry = () =>
  useMinerStore(useShallow((state) => [...state.telemetry.psus.values()].map(psuTelemetryView)));
export const usePsuTelemetry = (slot: number) =>
  useMinerStore((state) => {
    const value = state.telemetry.psus.get(asComponentId(`module:root:psu:${slot}`));
    return value ? psuTelemetryView(value) : undefined;
  });
export const useFansTelemetry = () =>
  useMinerStore(useShallow((state) => [...state.telemetry.fans.values()].map(fanTelemetryView)));
export const useFanTelemetry = (slot: number) =>
  useMinerStore((state) => {
    const value = state.telemetry.fans.get(asComponentId(`module:root:fan:${slot}`));
    return value ? fanTelemetryView(value) : undefined;
  });
export const useCoolingMode = () => useMinerStore((state) => state.telemetry.coolingModeByOwner.get(ROOT_MODULE_KEY));
export const useIntervalMs = () =>
  useMinerStore(
    (state) => state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.bucketSizeMs ?? 900_000,
  );
