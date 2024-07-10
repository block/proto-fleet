import { createContext } from "react";

import { Api, MiningStatusMiningstatus, Pool } from "./types";
import { FetchPoolsInfoProps } from "./usePoolsInfo";

const apiHost = import.meta.env.VITE_API_BASE_URL || "";
const { api } = new Api({ baseUrl: apiHost });

// TODO: remove this when done with development
(window as any).api = api;

export { api };
export const ApiContext = createContext({
  miningStatus: {} as MiningStatusMiningstatus,
  poolsInfo: [] as Pool[] | undefined,
  fetchPoolsInfo: ({ onSuccess, onError, retryOnMinerDown }: FetchPoolsInfoProps = {}) => {
    void onSuccess;
    void onError;
    void retryOnMinerDown;
  },
  poolsInfoStatus: { error: "", pending: false },
  setMiningStatus: (newMiningStatus: MiningStatusMiningstatus | undefined) => {
    void newMiningStatus;
  },
});
