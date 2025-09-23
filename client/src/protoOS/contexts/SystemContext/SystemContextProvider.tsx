import React, { ReactNode, useEffect, useMemo, useState } from "react";
import { SystemContext } from "./SystemContext";
import { useSystemInfo } from "@/protoOS/api/useSystemInfo";

interface SystemContextProviderProps {
  children: ReactNode;
  poll?: boolean;
  pollIntervalMs?: number;
}

const PROTO_RIG_MODEL_NAME = "Proto Rig";

// TODO: remove system context in favor of zustand store for consistency
export const SystemContextProvider: React.FC<SystemContextProviderProps> = ({
  children,
  poll = true,
}) => {
  const systemInfo = useSystemInfo({ poll });
  const [isProtoRig, setIsProtoRig] = useState<boolean>();

  useEffect(() => {
    if (!systemInfo.data?.product_name) {
      return;
    }
    setIsProtoRig(systemInfo.data.product_name === PROTO_RIG_MODEL_NAME);
  }, [systemInfo.data?.product_name]);

  const context = useMemo(
    () => ({ ...systemInfo, isProtoRig }),
    [systemInfo, isProtoRig],
  );

  return (
    <SystemContext.Provider value={context}>{children}</SystemContext.Provider>
  );
};
