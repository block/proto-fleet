import { useCallback, useMemo, useState } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { PoolAssignment } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors } from "@/protoFleet/store";

const useMinerPoolAssignments = () => {
  const { handleAuthErrors } = useAuthErrors();
  const [pools, setPools] = useState<PoolAssignment[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchPoolAssignments = useCallback(
    async (deviceIdentifier: string): Promise<PoolAssignment[]> => {
      setIsLoading(true);
      setError(null);

      try {
        const response = await fleetManagementClient.getMinerPoolAssignments({
          deviceIdentifier,
        });

        setPools(response.pools);
        return response.pools;
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            const errorMessage = err instanceof Error ? err.message : String(err);
            setError(errorMessage);
            console.error("Error fetching miner pool assignments:", err);
          },
        });
        return [];
      } finally {
        setIsLoading(false);
      }
    },
    [handleAuthErrors],
  );

  return useMemo(
    () => ({
      pools,
      isLoading,
      error,
      fetchPoolAssignments,
    }),
    [pools, isLoading, error, fetchPoolAssignments],
  );
};

export default useMinerPoolAssignments;
