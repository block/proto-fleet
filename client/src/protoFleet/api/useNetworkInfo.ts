import { useCallback, useEffect, useMemo, useState } from "react";
import { networkInfoClient } from "@/protoFleet/api/clients";
import {
  NetworkInfo,
  UpdateNetworkNicknameRequest,
} from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

interface UpdateNetworkInfoProps {
  networkUpdateRequest: UpdateNetworkNicknameRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

const useNetworkInfo = () => {
  const { authTokens } = useAuthContext();

  const [data, setData] = useState<NetworkInfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);

    networkInfoClient
      .getNetworkInfo({}, getAuthHeader(authTokens))
      .then((res) => {
        setData(res?.networkInfo);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [authTokens]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const updateNetworkInfo = useCallback(
    async ({
      networkUpdateRequest,
      onSuccess,
      onError,
    }: UpdateNetworkInfoProps) => {
      setPending(true);
      await networkInfoClient
        .updateNetworkNickname(networkUpdateRequest, getAuthHeader(authTokens))
        .then(() => {
          onSuccess();
        })
        .catch((err) => {
          onError?.(err?.error?.message ?? err);
        })
        .finally(() => {
          setPending(false);
        });
    },
    [authTokens],
  );

  return useMemo(
    () => ({ fetchData, pending, error, data, updateNetworkInfo }),
    [fetchData, pending, error, data, updateNetworkInfo],
  );
};

export { useNetworkInfo };
