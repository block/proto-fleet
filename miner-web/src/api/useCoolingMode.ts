import { useCallback } from "react";

import { api } from "./api";
import { CoolingConfig, MessageResponse } from "./types";

interface CoolingModeProps {
  onError?: (response: MessageResponse) => void;
  onSuccess?: () => void;
  fanMode: Required<CoolingConfig>["mode"];
}

const useCoolingMode = () => {
  const setCoolingMode = useCallback(
    async ({ fanMode, onSuccess, onError }: CoolingModeProps) => {
      await api
        .setCoolingMode({ mode: fanMode })
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          onError?.(err);
        })
    },
    []
  );

  return {
    setCoolingMode,
  };
};

export { useCoolingMode };
