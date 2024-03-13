import { createContext } from "react";

import { Api, Pool } from "./types";

const { api } = new Api();

// TODO: remove this when done with development
(window as any).api = api;

export { api };
export const ApiContext = createContext({
  poolsInfo: [] as Pool[],
  poolsInfoStatus: { pending: false },
});
