import { useCallback, useMemo } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import { useAuthErrors } from "@/protoFleet/store";

const useMinerCoolingMode = () => {
  const { handleAuthErrors } = useAuthErrors();

  const fetchCoolingMode = useCallback(
    async (deviceIdentifier: string): Promise<CoolingMode> => {
      try {
        const response = await fleetManagementClient.getMinerCoolingMode({
          deviceIdentifier,
        });

        return response.coolingMode;
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            console.error("Error fetching miner cooling mode:", err);
          },
        });
        return CoolingMode.UNSPECIFIED;
      }
    },
    [handleAuthErrors],
  );

  return useMemo(
    () => ({
      fetchCoolingMode,
    }),
    [fetchCoolingMode],
  );
};

export default useMinerCoolingMode;
