import { createContext } from "react";
import { SystemInfoSysteminfo } from "@/protoOS/api/types";

interface SystemContextValue {
  pending: boolean;
  error?: string;
  data?: SystemInfoSysteminfo;
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
