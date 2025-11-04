import { useCallback, useEffect, useMemo, useState } from "react";
import { networkInfoClient } from "@/protoFleet/api/clients";
import {
  NetworkInfo,
  UpdateNetworkNicknameRequest,
} from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import { useAuthErrors, useAuthHeader } from "@/protoFleet/store";

interface UpdateNetworkInfoProps {
  networkUpdateRequest: UpdateNetworkNicknameRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

const useNetworkInfo = () => {
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const [data, setData] = useState<NetworkInfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);

    networkInfoClient
      .getNetworkInfo({}, authHeader)
      .then((res) => {
        setData(res?.networkInfo);
      })
      .catch((err) => {
        handleAuthErrors({
          error: err,
          onError: () => {
            setError(err?.message ?? String(err));
          },
        });
      })
      .finally(() => {
        setPending(false);
      });
  }, [authHeader, handleAuthErrors]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
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
        .updateNetworkNickname(networkUpdateRequest, authHeader)
        .then(() => {
          onSuccess();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.error?.message ?? err);
            },
          });
        })
        .finally(() => {
          setPending(false);
        });
    },
    [authHeader, handleAuthErrors],
  );

  return useMemo(
    () => ({ fetchData, pending, error, data, updateNetworkInfo }),
    [fetchData, pending, error, data, updateNetworkInfo],
  );
};

export { useNetworkInfo };
