import { useCallback, useMemo, useState } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import { useAuthErrors } from "@/protoFleet/store";

interface MinerPoolAssignments {
  defaultPoolId: bigint | undefined;
  backup1PoolId: bigint | undefined;
  backup2PoolId: bigint | undefined;
}

const useMinerPoolAssignments = () => {
  const { handleAuthErrors } = useAuthErrors();
  const [assignments, setAssignments] = useState<MinerPoolAssignments | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchPoolAssignments = useCallback(
    async (deviceIdentifier: string): Promise<MinerPoolAssignments | null> => {
      setIsLoading(true);
      setError(null);

      try {
        const response = await fleetManagementClient.getMinerPoolAssignments({
          deviceIdentifier,
        });

        const assignments: MinerPoolAssignments = {
          defaultPoolId: response.defaultPoolId,
          backup1PoolId: response.backup1PoolId,
          backup2PoolId: response.backup2PoolId,
        };
        setAssignments(assignments);
        return assignments;
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: () => {
            const errorMessage = err instanceof Error ? err.message : String(err);
            setError(errorMessage);
            console.error("Error fetching miner pool assignments:", err);
          },
        });
        return null;
      } finally {
        setIsLoading(false);
      }
    },
    [handleAuthErrors],
  );

  return useMemo(
    () => ({
      assignments,
      isLoading,
      error,
      fetchPoolAssignments,
    }),
    [assignments, isLoading, error, fetchPoolAssignments],
  );
};

export default useMinerPoolAssignments;
