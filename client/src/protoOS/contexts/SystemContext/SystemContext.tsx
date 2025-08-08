import { createContext } from "react";

interface SystemContextValue {
  pending: boolean;
  error?: string;
  data?: any;
  processedData?: {
    isWebServerRunning: boolean;
    isMiningDriverRunning: boolean;
    hasFirmwareUpdate: boolean;
  };
  reload: () => void;
}

export const SystemContext = createContext<SystemContextValue | undefined>(
  undefined,
);
