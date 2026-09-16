import { useMinerStore } from "@/protoOS/store";
import type {
  ComponentRef,
  ControlBoardHardware,
  FanHardware,
  HashboardHardware,
  PsuHardware,
} from "@/protoOS/store/contract";

export type ComponentHardware = FanHardware | PsuHardware | HashboardHardware | ControlBoardHardware | undefined;

export function useComponentHardware(component: ComponentRef | undefined): ComponentHardware {
  return useMinerStore(({ hardware }) => {
    switch (component?.kind) {
      case "hashboard":
        return hardware.hashboards.get(component.id);
      case "psu":
        return hardware.psus.get(component.id);
      case "fan":
        return hardware.fans.get(component.id);
      case "controlBoard":
        return hardware.controlBoards.get(component.id);
      default:
        return undefined;
    }
  });
}
