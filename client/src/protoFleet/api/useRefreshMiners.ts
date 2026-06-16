import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  RefreshMinersRequestSchema,
  type RefreshMinersResponse,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors } from "@/protoFleet/store";

const MAX_REFRESH_MINERS = 50;

const useRefreshMiners = () => {
  const { handleAuthErrors } = useAuthErrors();
  const [refreshing, setRefreshing] = useState<Set<string>>(() => new Set());

  const refreshMiners = useCallback(
    async (deviceIds: string[]): Promise<RefreshMinersResponse> => {
      if (deviceIds.length === 0) {
        throw new Error("At least one miner is required.");
      }
      if (deviceIds.length > MAX_REFRESH_MINERS) {
        throw new Error(`Refresh is limited to ${MAX_REFRESH_MINERS} miners.`);
      }
      if (deviceIds.some((id) => id.trim() === "")) {
        throw new Error("Miner identifiers cannot be empty.");
      }

      setRefreshing((current) => {
        const next = new Set(current);
        deviceIds.forEach((id) => next.add(id));
        return next;
      });

      try {
        return await fleetManagementClient.refreshMiners(
          create(RefreshMinersRequestSchema, {
            deviceIds,
          }),
        );
      } catch (error) {
        handleAuthErrors({ error });
        throw error;
      } finally {
        setRefreshing((current) => {
          const next = new Set(current);
          deviceIds.forEach((id) => next.delete(id));
          return next;
        });
      }
    },
    [handleAuthErrors],
  );

  return useMemo(() => ({ refreshMiners, refreshing }), [refreshMiners, refreshing]);
};

export default useRefreshMiners;
