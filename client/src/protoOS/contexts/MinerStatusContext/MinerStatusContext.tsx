import { createContext, ReactNode, useEffect, useState } from "react";

import {
  ErrorListResponse,
  MiningStatusMiningstatus,
  Pool,
} from "../../api/types";
import { usePoll } from "@/protoOS/api";
import { FetchPoolsInfoProps, usePoolsInfo } from "@/protoOS/api/usePoolsInfo";

const MinerStatusContext = createContext({
  errors: {
    errors: undefined as ErrorListResponse | undefined,
    pending: false,
  },
  miningStatus: {} as MiningStatusMiningstatus,
  poolsInfo: [] as Pool[] | undefined,
  fetchPoolsInfo: ({
    onSuccess,
    onError,
    retryOnMinerDown,
  }: FetchPoolsInfoProps = {}) => {
    void onSuccess;
    void onError;
    void retryOnMinerDown;
  },
  poolsInfoStatus: { error: "", pending: false },
  setMiningStatus: (newMiningStatus: MiningStatusMiningstatus | undefined) => {
    void newMiningStatus;
  },
});

type MinerStatusProviderProps = {
  children: ReactNode;
  apiErrors?: ErrorListResponse;
  pendingErrors?: boolean;
  apiMiningStatus?: MiningStatusMiningstatus;
};

export const MinerStatusProvider = ({
  children,
  apiErrors,
  pendingErrors,
  apiMiningStatus,
}: MinerStatusProviderProps) => {
  const [miningStatus, setMiningStatus] = useState<
    MiningStatusMiningstatus | undefined
  >(apiMiningStatus);

  useEffect(() => {
    if (apiMiningStatus !== undefined) {
      setMiningStatus(apiMiningStatus);
    }
  }, [apiMiningStatus]);

  const {
    data: poolsInfo,
    error: errorPoolsInfo,
    fetchData: fetchPoolsInfo,
    pending: pendingPoolsInfo,
  } = usePoolsInfo();

  usePoll({
    fetchData: () => fetchPoolsInfo(),
    poll: true,
  });

  return (
    <MinerStatusContext.Provider
      value={{
        errors: {
          errors: apiErrors || [],
          pending: !!(pendingErrors && !apiErrors),
        },
        miningStatus: miningStatus || {},
        setMiningStatus,
        poolsInfo,
        fetchPoolsInfo,
        poolsInfoStatus: {
          error: errorPoolsInfo || "",
          pending: pendingPoolsInfo && !poolsInfo,
        },
      }}
    >
      {children}
    </MinerStatusContext.Provider>
  );
};

export default MinerStatusContext;
