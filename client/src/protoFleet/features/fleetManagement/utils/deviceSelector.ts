import { create } from "@bufbuild/protobuf";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { DeviceStatus, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DeviceFilterSchema,
  DeviceSelector,
  DeviceSelectorSchema,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { type SelectionMode } from "@/shared/components/List";

export interface DeviceFilterCriteria {
  deviceStatus?: DeviceStatus;
  pairingStatus?: PairingStatus;
}

/**
 * Creates a DeviceSelector based on the selection mode.
 * - "all": uses allDevices with optional filter criteria to target filtered miners
 * - "subset": uses includeDevices with specific device identifiers
 * - "none": throws an error (callers should disable actions when nothing is selected)
 */
export const createDeviceSelector = (
  selectionMode: SelectionMode,
  deviceIdentifiers: string[],
  filterCriteria?: DeviceFilterCriteria,
): DeviceSelector => {
  if (selectionMode === "none") {
    throw new Error("Cannot create DeviceSelector with no selection");
  }
  if (selectionMode === "all") {
    return create(DeviceSelectorSchema, {
      selectionType: {
        case: "allDevices",
        value: create(DeviceFilterSchema, {
          deviceStatus: filterCriteria?.deviceStatus ? [filterCriteria.deviceStatus] : [],
          pairingStatus: filterCriteria?.pairingStatus ? [filterCriteria.pairingStatus] : [],
        }),
      },
    });
  }
  return create(DeviceSelectorSchema, {
    selectionType: {
      case: "includeDevices",
      value: create(DeviceIdentifierListSchema, {
        deviceIdentifiers,
      }),
    },
  });
};
