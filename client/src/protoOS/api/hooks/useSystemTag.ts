import { useCallback, useMemo } from "react";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry, useSetSystemTag, useSystemTagValue } from "@/protoOS/store";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";

const useSystemTag = () => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const systemTag = useSystemTagValue();
  const setSystemTag = useSetSystemTag();

  const getSystemTag = useCallback(
    ({ onSuccess, onError }: { onSuccess?: (tag: string) => void; onError?: (message: string) => void }) => {
      if (!adapter) return;
      const accept = beginStoreWrite("systemInfo:tag");

      adapter
        .getSystemTag()
        .then((tag) => {
          if (!accept()) return;
          setSystemTag(tag);
          onSuccess?.(tag);
        })
        .catch((err) => {
          if (err?.status === 404) {
            onSuccess?.("");
            return;
          }
          onError?.(err?.error?.message ?? "Failed to get system tag");
        });
    },
    [adapter, setSystemTag],
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
      if (!adapter) return;
      const accept = beginStoreWrite("systemInfo:tag");

      authRetry({
        request: (params) => adapter.setSystemTag(value, params),
        onSuccess: () => {
          if (accept()) setSystemTag(value);
          onSuccess?.();
        },
        onError: (err) => onError?.(err?.error?.message ?? "Failed to save system tag"),
      });
    },
    [adapter, authRetry, setSystemTag],
  );

  return useMemo(() => ({ systemTag, getSystemTag, putSystemTag }), [systemTag, getSystemTag, putSystemTag]);
};

export { useSystemTag };
