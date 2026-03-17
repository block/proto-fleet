import { useCallback, useMemo } from "react";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store";

const useSystemTag = () => {
  const { api } = useMinerHosting();
  const authRetry = useAuthRetry();

  const getSystemTag = useCallback(
    ({ onSuccess, onError }: { onSuccess?: (tag: string) => void; onError?: (message: string) => void }) => {
      if (!api) return;

      api
        .getSystemTag()
        .then((res) => {
          onSuccess?.(typeof res.data === "string" ? res.data : JSON.stringify(res.data));
        })
        .catch((err) => {
          if (err?.status === 404) {
            onSuccess?.("");
            return;
          }
          onError?.(err?.error?.message ?? "Failed to get system tag");
        });
    },
    [api],
  );

  const putSystemTag = useCallback(
    (
      value: string,
      {
        onSuccess,
        onError,
      }: {
        onSuccess?: () => void;
        onError?: (message: string) => void;
      },
    ) => {
      if (!api) return;

      authRetry({
        request: (header) => api.putSystemTag(value, header),
        onSuccess: () => onSuccess?.(),
        onError: (err) => onError?.(err?.error?.message ?? "Failed to save system tag"),
      });
    },
    [api, authRetry],
  );

  return useMemo(() => ({ getSystemTag, putSystemTag }), [getSystemTag, putSystemTag]);
};

export { useSystemTag };
