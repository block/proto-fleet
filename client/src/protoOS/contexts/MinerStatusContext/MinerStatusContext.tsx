import { createContext, ReactNode, useEffect, useState } from "react";

import { FetchPoolsInfoProps, usePoll, usePoolsInfo } from "@/protoOS/api";
import {
  ErrorListResponse,
  MiningStatusMiningstatus,
  Pool,
} from "@/protoOS/api/generatedApi";

type WakeDialog = {
  show: boolean;
  onConfirm: () => void;
  onClose: () => void;
};

const MinerStatusContext = createContext({
  errors: {
    errors: undefined as ErrorListResponse | undefined,
    pending: false,
  },
  miningStatus: {} as MiningStatusMiningstatus | undefined,
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
  wakeDialog: {
    show: false,
    onConfirm: () => {},
    onClose: () => {},
  } as WakeDialog,
  showWakeDialog: (onConfirm: () => void, onClose: () => void) => {
    void onConfirm;
    void onClose;
  },
  hideWakeDialog: () => {},
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

  const [wakeDialog, setWakeDialog] = useState<WakeDialog>({
    show: false,
    onConfirm: () => {},
    onClose: () => {},
  });

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

  const showWakeDialog = (onConfirm: () => void, onClose: () => void) => {
    setWakeDialog({ show: true, onConfirm, onClose });
  };

  const hideWakeDialog = () => {
    setWakeDialog({ show: false, onConfirm: () => {}, onClose: () => {} });
  };

  return (
    <MinerStatusContext.Provider
      value={{
        errors: {
          errors: apiErrors || [],
          pending: !!(pendingErrors && !apiErrors),
        },
        miningStatus: miningStatus,
        setMiningStatus,
        poolsInfo,
        fetchPoolsInfo,
        poolsInfoStatus: {
          error: errorPoolsInfo || "",
          pending: pendingPoolsInfo && !poolsInfo,
        },
        wakeDialog,
        showWakeDialog,
        hideWakeDialog,
      }}
    >
      {children}
    </MinerStatusContext.Provider>
  );
};

export default MinerStatusContext;
