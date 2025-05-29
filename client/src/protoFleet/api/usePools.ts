import { useCallback, useEffect, useMemo, useState } from "react";
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

interface ValidatePoolProps {
  validatePoolRequest: ValidatePoolRequest;
  onSuccess?: () => void;
  onError?: (error: string) => void;
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

  const validatePool = useCallback(
    async ({ validatePoolRequest, onSuccess, onError }: ValidatePoolProps) => {
      await poolsClient
        .validatePool(validatePoolRequest, getAuthHeader(authTokens))
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
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
    }),
    [pools, setDefaultPool, createPool, updatePool, deletePool, validatePool],
  );
};

export default usePools;
