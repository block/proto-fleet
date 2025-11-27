import { useCallback, useEffect, useMemo, useState } from "react";
import { Duration } from "@bufbuild/protobuf/wkt";
import { poolsClient } from "@/protoFleet/api/clients";
import type {
  CreatePoolRequest,
  DeletePoolRequest,
  ListPoolsResponse,
  SetDefaultPoolRequest,
  UpdatePoolRequest,
  ValidatePoolRequest,
} from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { useAuthErrors, useAuthHeader } from "@/protoFleet/store";

interface SetDefaultPoolProps {
  defaultPoolRequest: SetDefaultPoolRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

interface CreatePoolProps {
  createPoolRequest: CreatePoolRequest;
  onSuccess?: () => void;
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

const usePools = () => {
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const [pools, setPools] = useState<ListPoolsResponse["pools"]>([]);

  const fetchPools = useCallback(async () => {
    try {
      const response = await poolsClient.listPools({}, authHeader);

      setPools(response.pools);
    } catch (error) {
      handleAuthErrors({
        error: error,
        onError: () => {
          console.error("Error fetching pools:", error);
          throw error;
        },
      });
    }
  }, [authHeader, setPools, handleAuthErrors]);

  useEffect(() => {
    fetchPools();
  }, [fetchPools]);

  const setDefaultPool = useCallback(
    async ({ defaultPoolRequest, onSuccess, onError }: SetDefaultPoolProps) => {
      await poolsClient
        .setDefaultPool(defaultPoolRequest, authHeader)
        .then(() => {
          onSuccess();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
  );

  const createPool = useCallback(
    async ({ createPoolRequest, onSuccess, onError }: CreatePoolProps) => {
      await poolsClient
        .createPool(createPoolRequest, authHeader)
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
  );

  const updatePool = useCallback(
    async ({ updatePoolRequest, onSuccess, onError }: UpdatePoolProps) => {
      await poolsClient
        .updatePool(updatePoolRequest, authHeader)
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
  );

  const deletePool = useCallback(
    async ({ deletePoolRequest, onSuccess, onError }: DeletePoolProps) => {
      await poolsClient
        .deletePool(deletePoolRequest, authHeader)
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        });
    },
    [authHeader, handleAuthErrors],
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
        .validatePool(request, authHeader)
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
          setValidatePoolPending(false);
        });
    },
    [authHeader, handleAuthErrors],
  );

  return useMemo(
    () => ({
      pools,
      setDefaultPool,
      createPool,
      updatePool,
      deletePool,
      validatePool,
      validatePoolPending,
    }),
    [pools, setDefaultPool, createPool, updatePool, deletePool, validatePool, validatePoolPending],
  );
};

export default usePools;
