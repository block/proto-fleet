import { createContext } from "react";

import { Api, MiningStatusMiningstatus, Pool } from "./types";

const apiHost = import.meta.env.VITE_API_BASE_URL || "";
const { api } = new Api({ baseUrl: apiHost });

// TODO: remove this when done with development
(window as any).api = api;

export { api };
export const ApiContext = createContext({
  miningStatus: {} as MiningStatusMiningstatus,
  poolsInfo: [] as Pool[],
  poolsInfoStatus: { pending: false },
  setMiningStatus: (newMiningStatus: MiningStatusMiningstatus | undefined) => {
    void newMiningStatus;
  },
});
