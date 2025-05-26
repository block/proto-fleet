import { useCallback, useEffect, useMemo, useState } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import type {
  CreatePoolRequest,
  DeletePoolRequest,
  ListPoolsResponse,
  SetDefaultPoolRequest,
  UpdatePoolRequest,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
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

const usePools = () => {
  const { authTokens } = useAuthContext();

  const [pools, setPools] = useState<ListPoolsResponse["pools"]>([]);

  const fetchPools = useCallback(async () => {
    try {
      const response = await fleetManagementClient.listPools(
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
      await fleetManagementClient
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
      await fleetManagementClient
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
      await fleetManagementClient
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
      await fleetManagementClient
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

  return useMemo(
    () => ({
      pools,
      setDefaultPool,
      createPool,
      updatePool,
      deletePool,
    }),
    [pools, setDefaultPool, createPool, updatePool, deletePool],
  );
};

export default usePools;
