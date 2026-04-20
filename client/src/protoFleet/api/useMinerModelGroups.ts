import { useCallback } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  type MinerListFilter,
  type MinerModelGroup,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const useMinerModelGroups = () => {
  const getMinerModelGroups = useCallback(async (filter: MinerListFilter | null): Promise<MinerModelGroup[]> => {
    const response = await fleetManagementClient.getMinerModelGroups({
      filter: filter ?? undefined,
    });
    return response.groups;
  }, []);

  return { getMinerModelGroups };
};

export default useMinerModelGroups;
