import React, { ReactNode } from "react";
import { SystemContext } from "./SystemContext";
import { useSystemInfo } from "@/protoOS/api/useSystemInfo";

interface SystemContextProviderProps {
  children: ReactNode;
  poll?: boolean;
  pollIntervalMs?: number;
}

export const SystemContextProvider: React.FC<SystemContextProviderProps> = ({
  children,
  poll = true,
}) => {
  const systemInfo = useSystemInfo({ poll });

  return (
    <SystemContext.Provider value={systemInfo}>
      {children}
    </SystemContext.Provider>
  );
};
