import { createContext } from "react";

import {
  ErrorListResponse,
  MiningStatusMiningstatus,
  Pool,
} from "../../api/types";
import { FetchPoolsInfoProps } from "../../api/usePoolsInfo";

export const MinerStatusContext = createContext({
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
