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
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

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
  const { authTokens } = useAuthContext();

  const [pools, setPools] = useState<ListPoolsResponse["pools"]>([]);

  const fetchPools = useCallback(async () => {
    try {
      const response = await poolsClient.listPools(
        {},
        getAuthHeader(authTokens),
      );

      setPools(response.pools);
    } catch (error) {
      console.error("Error fetching pools:", error);
      throw error;
    }
  }, [authTokens, setPools]);

  useEffect(() => {
    fetchPools();
  }, [fetchPools]);

  const setDefaultPool = useCallback(
    async ({ defaultPoolRequest, onSuccess, onError }: SetDefaultPoolProps) => {
      await poolsClient
        .setDefaultPool(defaultPoolRequest, getAuthHeader(authTokens))
        .then(() => {
          onSuccess();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        });
    },
    [authTokens],
  );

  const createPool = useCallback(
    async ({ createPoolRequest, onSuccess, onError }: CreatePoolProps) => {
      await poolsClient
        .createPool(createPoolRequest, getAuthHeader(authTokens))
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        });
    },
    [authTokens],
  );

  const updatePool = useCallback(
    async ({ updatePoolRequest, onSuccess, onError }: UpdatePoolProps) => {
      await poolsClient
        .updatePool(updatePoolRequest, getAuthHeader(authTokens))
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        });
    },
    [authTokens],
  );

  const deletePool = useCallback(
    async ({ deletePoolRequest, onSuccess, onError }: DeletePoolProps) => {
      await poolsClient
        .deletePool(deletePoolRequest, getAuthHeader(authTokens))
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        });
    },
    [authTokens],
  );

  const [validatePoolPending, setValidatePoolPending] = useState(false);
  const validatePool = useCallback(
    async ({ poolInfo, onSuccess, onError, onFinally }: ValidatePoolProps) => {
      setValidatePoolPending(true);

      // Create request object, only include password if it's not empty
      const request: Omit<ValidatePoolRequest, "$typeName"> = {
        url: poolInfo.url,
        username: poolInfo.username,
        ...(poolInfo.password &&
          poolInfo.password.trim() && { password: poolInfo.password }),
        ...(poolInfo.timeout && {
          timeout: poolInfo.timeout as Duration,
        }),
      };

      await poolsClient
        .validatePool(request, getAuthHeader(authTokens))
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        })
        .finally(() => {
          onFinally?.();
          setValidatePoolPending(false);
        });
    },
    [authTokens],
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
    [
      pools,
      setDefaultPool,
      createPool,
      updatePool,
      deletePool,
      validatePool,
      validatePoolPending,
    ],
  );
};

export default usePools;
