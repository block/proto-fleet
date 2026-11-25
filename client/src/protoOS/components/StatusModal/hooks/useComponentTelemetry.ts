import { useMinerStore } from "@/protoOS/store";
import {
  asEntityKey,
  type ComponentRef,
  type FanTelemetry,
  type HashboardTelemetry,
  type LevelRollup,
  type PsuTelemetry,
} from "@/protoOS/store/contract";

export type ComponentTelemetry = FanTelemetry | PsuTelemetry | HashboardTelemetry | LevelRollup | undefined;

export function useComponentTelemetry(component: ComponentRef | undefined): ComponentTelemetry {
  return useMinerStore(({ telemetry }) => {
    switch (component?.kind) {
      case "hashboard":
        return telemetry.hashboards.get(component.id);
      case "psu":
        return telemetry.psus.get(component.id);
      case "fan":
        return telemetry.fans.get(component.id);
      case "controlBoard":
        return telemetry.rollupsByEntity.get(asEntityKey(component.owner.id));
      default:
        return undefined;
    }
  });
}
