import { useCallback, useEffect, useMemo, useState } from "react";
import { Duration } from "@bufbuild/protobuf/wkt";
import { poolsClient } from "@/protoFleet/api/clients";
import type {
  CreatePoolRequest,
  DeletePoolRequest,
  ListPoolsResponse,
  UpdatePoolRequest,
  ValidatePoolRequest,
  ValidationMode,
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

// ValidatePoolOutcome carries every field the UI may want to render so the
// button isn't lying about what was actually verified. SV2 probes that
// only completed a TCP dial come back reachable=true, credentialsVerified=
// false; SV1 authenticate flows come back reachable=true, credentialsVerified=
// true. The mode lets the UI show the operator which kind of check ran.
export interface ValidatePoolOutcome {
  reachable: boolean;
  credentialsVerified: boolean;
  mode: ValidationMode;
}

export interface ValidatePoolProps {
  // noisePublicKey is meaningful only for SV2 URLs (the server detects
  // protocol from the URL scheme and switches to handshake-probe mode
  // when a key is present). Optional everywhere else.
  poolInfo: Omit<ValidatePoolRequest, "$typeName" | "noisePublicKey"> & {
    noisePublicKey?: Uint8Array;
  };
  onSuccess?: (outcome: ValidatePoolOutcome) => void;
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

      // Protocol is derived server-side from the URL scheme. Only
      // include password / noise pubkey / timeout when actually set.
      const request: Omit<ValidatePoolRequest, "$typeName"> = {
        url: poolInfo.url,
        username: poolInfo.username,
        ...(poolInfo.password && poolInfo.password.trim() && { password: poolInfo.password }),
        ...(poolInfo.noisePublicKey &&
          poolInfo.noisePublicKey.byteLength > 0 && {
            noisePublicKey: poolInfo.noisePublicKey,
          }),
        ...(poolInfo.timeout && {
          timeout: poolInfo.timeout as Duration,
        }),
      };

      await poolsClient
        .validatePool(request)
        .then((response) => {
          onSuccess?.({
            reachable: response.reachable,
            credentialsVerified: response.credentialsVerified,
            mode: response.mode,
          });
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
