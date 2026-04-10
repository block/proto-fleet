import { useCallback, useEffect, useMemo, useState } from "react";
import { Duration } from "@bufbuild/protobuf/wkt";
import { poolsClient } from "@/protoFleet/api/clients";
import type {
  CreatePoolRequest,
  DeletePoolRequest,
  ListPoolsResponse,
  UpdatePoolRequest,
  ValidatePoolRequest,
} from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface CreatePoolProps {
  createPoolRequest: CreatePoolRequest;
  onSuccess?: (poolId: string) => void;
  onError?: (error: string) => void;
}

interface UpdatePoolProps {
  updatePoolRequest: UpdatePoolRequest;
  onSuccess?: () => void;
  onError?: (error: string) => void;
}

interface DeletePoolProps {
  deletePoolRequest: DeletePoolRequest;
  onSuccess?: () => void;
  onError?: (error: string) => void;
}

export interface ValidatePoolProps {
  poolInfo: Omit<ValidatePoolRequest, "$typeName">;
  onSuccess?: () => void;
  onError?: (error: string) => void;
  onFinally?: () => void;
}

const usePools = (enabled = true) => {
  const { handleAuthErrors } = useAuthErrors();

  const [pools, setPools] = useState<ListPoolsResponse["pools"]>([]);
  const [isLoading, setIsLoading] = useState(true);

  const fetchPools = useCallback(
    async (showLoading = true) => {
      try {
        if (showLoading) {
          setIsLoading(true);
        }
        const response = await poolsClient.listPools({});

        setPools(response.pools);
      } catch (error) {
        handleAuthErrors({
          error: error,
          onError: () => {
            console.error("Error fetching pools:", error);
            throw error;
          },
        });
      } finally {
        if (showLoading) {
          setIsLoading(false);
        }
      }
    },
    [setPools, handleAuthErrors],
  );

  useEffect(() => {
    if (!enabled) {
      setIsLoading(false);
      return;
    }

    fetchPools();
  }, [enabled, fetchPools]);

  const createPool = useCallback(
    async ({ createPoolRequest, onSuccess, onError }: CreatePoolProps) => {
      await poolsClient
        .createPool(createPoolRequest)
        .then((response) => {
          if (!response.pool || !response.pool.poolId) {
            onError?.("Pool created but no pool ID returned");
            return;
          }

          const pool = response.pool;
          const poolId = pool.poolId;

          setPools((prevPools) => [...prevPools, pool]);

          onSuccess?.(poolId.toString());
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const updatePool = useCallback(
    async ({ updatePoolRequest, onSuccess, onError }: UpdatePoolProps) => {
      await poolsClient
        .updatePool(updatePoolRequest)
        .then(() => {
          fetchPools(false); // Don't show loading spinner on refetch
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors, fetchPools],
  );

  const deletePool = useCallback(
    async ({ deletePoolRequest, onSuccess, onError }: DeletePoolProps) => {
      await poolsClient
        .deletePool(deletePoolRequest)
        .then(() => {
          setPools((prevPools) => prevPools.filter((pool) => pool.poolId !== deletePoolRequest.poolId));
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        });
    },
    [handleAuthErrors],
  );

  const [validatePoolPending, setValidatePoolPending] = useState(false);
  const validatePool = useCallback(
    async ({ poolInfo, onSuccess, onError, onFinally }: ValidatePoolProps) => {
      setValidatePoolPending(true);

      // Create request object, only include password if it's not empty
      const request: Omit<ValidatePoolRequest, "$typeName"> = {
        url: poolInfo.url,
        username: poolInfo.username,
        ...(poolInfo.password && poolInfo.password.trim() && { password: poolInfo.password }),
        ...(poolInfo.timeout && {
          timeout: poolInfo.timeout as Duration,
        }),
      };

      await poolsClient
        .validatePool(request)
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
          setValidatePoolPending(false);
        });
    },
    [handleAuthErrors],
  );

  // Sort pools by name (case-insensitive) for consistent display
  const sortedPools = useMemo(
    () => [...pools].sort((a, b) => a.poolName.localeCompare(b.poolName, undefined, { sensitivity: "base" })),
    [pools],
  );

  const miningPools = useMemo(
    () =>
      sortedPools.map((pool) => ({
        poolId: pool.poolId.toString(),
        name: pool.poolName,
        poolUrl: pool.url,
        username: pool.username,
      })),
    [sortedPools],
  );

  return useMemo(
    () => ({
      pools: sortedPools,
      miningPools,
      createPool,
      updatePool,
      deletePool,
      validatePool,
      validatePoolPending,
      isLoading,
    }),
    [sortedPools, miningPools, createPool, updatePool, deletePool, validatePool, validatePoolPending, isLoading],
  );
};

export default usePools;
