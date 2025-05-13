import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "./apiResponseTypes";
import {
  CoolingConfig,
  CoolingStatusCoolingstatus,
  HttpResponse,
} from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/features/auth/contexts/AuthContext";

interface UseCoolingStatusProps {
  poll?: boolean;
}

interface SetCoolingProps {
  mode: CoolingConfig["mode"];
  accessTokenValue?: string;
  onSuccess?: (res: HttpResponse<CoolingConfig>) => void;
  onError?: (error: ErrorProps) => void;
}

const useCoolingStatus = ({ poll }: UseCoolingStatusProps = {}) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<CoolingStatusCoolingstatus>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getCooling()
      .then((res) => {
        setData(res?.data["cooling-status"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [api]);

  usePoll({
    fetchData,
    poll,
  });

  const setCooling = useCallback(
    async ({ mode, accessTokenValue, onSuccess, onError }: SetCoolingProps) => {
      if (!api) return;
      setPending(true);
      await api
        .setCoolingMode(
          { mode },
          getAuthHeader(accessTokenValue || authTokens.accessToken.value),
        )
        .then(async (res) => {
          const data = await res.json();
          setData(data.mode);
          setPending(false);
          onSuccess?.(data);
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            onError: (error) => {
              onError?.(error), setPending(false);
            },
            onSuccess: (accessTokenValue) => {
              setCooling({ mode, accessTokenValue, onSuccess, onError });
            },
          });
        });
    },
    [api, authTokens.accessToken.value, handleAuthErrors],
  );

  return useMemo(
    () => ({ pending, error, data, setCooling }),
    [pending, error, data, setCooling],
  );
};

export { useCoolingStatus };
