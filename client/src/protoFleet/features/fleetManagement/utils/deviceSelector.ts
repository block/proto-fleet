import { create } from "@bufbuild/protobuf";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  DeviceStatus,
  type MinerListFilter,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
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
 * - "all" + `minerListFilter`: uses allMatchingFilter so a bulk command targets
 *   the full filtered set across pages (the server resolves the rich filter and
 *   defaults to command-eligible miners). Use this whenever a filter is active.
 * - "all" without `minerListFilter`: uses allDevices with optional thin filter
 *   criteria (whole fleet — the unfiltered select-all case).
 * - "subset": uses includeDevices with specific device identifiers
 * - "none": throws an error (callers should disable actions when nothing is selected)
 */
export const createDeviceSelector = (
  selectionMode: SelectionMode,
  deviceIdentifiers: string[],
  filterCriteria?: DeviceFilterCriteria,
  minerListFilter?: MinerListFilter,
): DeviceSelector => {
  if (selectionMode === "none") {
    throw new Error("Cannot create DeviceSelector with no selection");
  }
  if (selectionMode === "all") {
    if (minerListFilter) {
      return create(DeviceSelectorSchema, {
        selectionType: {
          case: "allMatchingFilter",
          value: minerListFilter,
        },
      });
    }
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
