import { useCallback, useMemo } from "react";
import { create } from "@bufbuild/protobuf";

import { fleetManagementClient } from "@/protoFleet/api/clients";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { type SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type DeviceSelector,
  DeviceSelectorSchema,
  type MinerNameConfig,
  MinerNameConfigSchema,
  NamePropertySchema,
  RenameMinersRequestSchema,
  type RenameMinersResponse,
  StringPropertySchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors } from "@/protoFleet/store";

const useRenameMiners = () => {
  const { handleAuthErrors } = useAuthErrors();

  const renameMiners = useCallback(
    async (
      deviceSelector: DeviceSelector,
      nameConfig: MinerNameConfig,
      sort?: SortConfig,
    ): Promise<RenameMinersResponse> => {
      try {
        return await fleetManagementClient.renameMiners(
          create(RenameMinersRequestSchema, {
            deviceSelector,
            nameConfig,
            sort: sort ? [sort] : [],
          }),
        );
      } catch (err) {
        handleAuthErrors({
          error: err,
        });
        throw err;
      }
    },
    [handleAuthErrors],
  );

  const renameSingleMiner = useCallback(
    async (deviceIdentifier: string, name: string) => {
      await renameMiners(
        create(DeviceSelectorSchema, {
          selectionType: {
            case: "includeDevices",
            value: create(DeviceIdentifierListSchema, { deviceIdentifiers: [deviceIdentifier] }),
          },
        }),
        create(MinerNameConfigSchema, {
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
      );
    },
    [renameMiners],
  );

  return useMemo(() => ({ renameMiners, renameSingleMiner }), [renameMiners, renameSingleMiner]);
};

export default useRenameMiners;
