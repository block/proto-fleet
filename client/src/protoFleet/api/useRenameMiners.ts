import { useCallback, useMemo } from "react";
import { create } from "@bufbuild/protobuf";

import { fleetManagementClient } from "@/protoFleet/api/clients";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  DeviceSelectorSchema,
  MinerNameConfigSchema,
  NamePropertySchema,
  RenameMinersRequestSchema,
  StringPropertySchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors } from "@/protoFleet/store";

const useRenameMiners = () => {
  const { handleAuthErrors } = useAuthErrors();

  const renameSingleMiner = useCallback(
    async (deviceIdentifier: string, name: string) => {
      try {
        await fleetManagementClient.renameMiners(
          create(RenameMinersRequestSchema, {
            deviceSelector: create(DeviceSelectorSchema, {
              selectionType: {
                case: "includeDevices",
                value: create(DeviceIdentifierListSchema, { deviceIdentifiers: [deviceIdentifier] }),
              },
            }),
            nameConfig: create(MinerNameConfigSchema, {
              properties: [
                create(NamePropertySchema, {
                  kind: {
                    case: "stringValue",
                    value: create(StringPropertySchema, { value: name }),
                  },
                }),
              ],
              separator: "",
            }),
          }),
        );
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            throw err;
          },
        });
      }
    },
    [handleAuthErrors],
  );

  return useMemo(() => ({ renameSingleMiner }), [renameSingleMiner]);
};

export default useRenameMiners;
