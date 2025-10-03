import { createContext } from "react";
import { SystemInfoSysteminfo } from "@/protoOS/api/generatedApi";

interface SystemContextValue {
  pending: boolean;
  error?: string;
  data?: SystemInfoSysteminfo;
  isProtoRig?: boolean;
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
