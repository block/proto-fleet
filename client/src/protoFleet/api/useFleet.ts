import { useCallback, useEffect, useMemo, useState } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  type CreatePoolRequest,
  type ListPairedMinersRequest,
  type ListPairedMinersResponse,
  type SetDefaultPoolRequest,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

type FetchPairedMinersArgs = {
  pageSize?: ListPairedMinersRequest["pageSize"];
  cursor?: ListPairedMinersRequest["cursor"];
};

interface SetDefaultPoolProps {
  defaultPoolRequest: SetDefaultPoolRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

interface CreatePoolProps {
  createPoolRequest: CreatePoolRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

const useFleet = () => {
  const { authTokens } = useAuthContext();

  const [miners, setMiners] = useState<ListPairedMinersResponse["miners"]>([]);
  const [cursor, setCursor] = useState<ListPairedMinersResponse["cursor"]>("");
  const [totalMiners, setTotalMiners] =
    useState<ListPairedMinersResponse["totalMiners"]>();

  void totalMiners; // not using this yet, but keeping it for potential future use

  const fetchPairedMiners = useCallback(
    async ({ pageSize }: FetchPairedMinersArgs) => {
      try {
        const response = await fleetManagementClient.listPairedMiners(
          { pageSize, cursor },
          getAuthHeader(authTokens),
        );

        const { miners, cursor: newCursor, totalMiners } = response;
        setMiners(miners);
        setCursor(newCursor);
        setTotalMiners(totalMiners);
      } catch (error) {
        console.error("Error fetching fleet data:", error);
        throw error;
      }
    },
    [cursor, authTokens, setMiners, setCursor, setTotalMiners],
  );

  useEffect(() => {
    fetchPairedMiners({ pageSize: 100 });
  }, [fetchPairedMiners]);

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
          onSuccess();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        });
    },
    [authTokens],
  );

  return useMemo(
    () => ({ miners, setDefaultPool, createPool }),
    [miners, setDefaultPool, createPool],
  );
};

export default useFleet;
