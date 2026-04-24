import { useCallback, useEffect, useMemo, useState } from "react";
import { networkInfoClient } from "@/protoFleet/api/clients";
import { NetworkInfo, UpdateNetworkNicknameRequest } from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface UpdateNetworkInfoProps {
  networkUpdateRequest: UpdateNetworkNicknameRequest;
  onSuccess: () => void;
  onError?: (error: string) => void;
}

const useNetworkInfo = () => {
  const { handleAuthErrors } = useAuthErrors();

  const [data, setData] = useState<NetworkInfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);

    networkInfoClient
      .getNetworkInfo({})
      .then((res) => {
        setData(res?.networkInfo);
      })
      .catch((err) => {
        handleAuthErrors({
          error: err,
          onError: () => {
            setError(getErrorMessage(err));
          },
        });
      })
      .finally(() => {
        setPending(false);
      });
  }, [handleAuthErrors]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    fetchData();
  }, [fetchData]);

  const updateNetworkInfo = useCallback(
    async ({ networkUpdateRequest, onSuccess, onError }: UpdateNetworkInfoProps) => {
      setPending(true);
      await networkInfoClient
        .updateNetworkNickname(networkUpdateRequest)
        .then(() => {
          onSuccess();
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
          setPending(false);
        });
    },
    [handleAuthErrors],
  );

  return useMemo(
    () => ({ fetchData, pending, error, data, updateNetworkInfo }),
    [fetchData, pending, error, data, updateNetworkInfo],
  );
};

export { useNetworkInfo };
