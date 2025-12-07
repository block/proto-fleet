import { create } from "@bufbuild/protobuf";
import {
  DeviceListSchema,
  DeviceSelector,
  DeviceSelectorSchema,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { type SelectionMode } from "@/shared/components/List";

/**
 * Creates a DeviceSelector based on the selection mode.
 * - "all": uses allDevices=true to target all miners in the fleet
 * - "subset": uses includeDevices with specific device identifiers
 * - "none": throws an error (callers should disable actions when nothing is selected)
 */
export const createDeviceSelector = (selectionMode: SelectionMode, deviceIdentifiers: string[]): DeviceSelector => {
  if (selectionMode === "none") {
    throw new Error("Cannot create DeviceSelector with no selection");
  }
  if (selectionMode === "all") {
    return create(DeviceSelectorSchema, {
      selectionType: {
        case: "allDevices",
        value: true,
      },
    });
  }
  return create(DeviceSelectorSchema, {
    selectionType: {
      case: "includeDevices",
      value: create(DeviceListSchema, {
        deviceIdentifiers,
      }),
    },
  });
};
