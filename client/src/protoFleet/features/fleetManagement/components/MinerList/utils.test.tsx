import { describe, expect, it } from "vitest";
import { getComponentIcon } from "./utils";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { Alert, ControlBoard, Fan, Hashboard, LightningAlt } from "@/shared/assets/icons";

describe("getComponentIcon", () => {
  it.each([
    ["UNSPECIFIED", ErrorComponentType.UNSPECIFIED, Alert],
    ["PSU", ErrorComponentType.PSU, LightningAlt],
    ["HASH_BOARD", ErrorComponentType.HASH_BOARD, Hashboard],
    ["FAN", ErrorComponentType.FAN, Fan],
    ["CONTROL_BOARD", ErrorComponentType.CONTROL_BOARD, ControlBoard],
    ["EEPROM", ErrorComponentType.EEPROM, Alert],
    ["IO_MODULE", ErrorComponentType.IO_MODULE, Alert],
    ["an unmapped value", 999 as ErrorComponentType, Alert],
  ])("returns the expected icon for %s", (_name, componentType, expectedIcon) => {
    expect(getComponentIcon(componentType)).toEqual(expect.objectContaining({ type: expectedIcon }));
  });
});
