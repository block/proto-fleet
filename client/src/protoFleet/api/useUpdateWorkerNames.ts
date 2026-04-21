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
  StringPropertySchema,
  UpdateWorkerNamesRequestSchema,
  type UpdateWorkerNamesResponse,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors } from "@/protoFleet/store";

const useUpdateWorkerNames = () => {
  const { handleAuthErrors } = useAuthErrors();

  const updateWorkerNames = useCallback(
    async (
      deviceSelector: DeviceSelector,
      nameConfig: MinerNameConfig,
      userUsername: string,
      userPassword: string,
      sort?: SortConfig,
    ): Promise<UpdateWorkerNamesResponse> => {
      try {
        return await fleetManagementClient.updateWorkerNames(
          create(UpdateWorkerNamesRequestSchema, {
            deviceSelector,
            nameConfig,
            sort: sort ? [sort] : [],
            userUsername,
            userPassword,
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

  const updateSingleWorkerName = useCallback(
    async (deviceIdentifier: string, name: string, userUsername: string, userPassword: string) =>
      updateWorkerNames(
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
        userUsername,
        userPassword,
      ),
    [updateWorkerNames],
  );

  return useMemo(() => ({ updateWorkerNames, updateSingleWorkerName }), [updateSingleWorkerName, updateWorkerNames]);
};

export default useUpdateWorkerNames;
