import { createContext } from "react";

import { Api, Pool } from "./types";

const apiHost = import.meta.env.VITE_API_BASE_URL || "http://";
const { api } = new Api({ baseUrl: apiHost });

// TODO: remove this when done with development
(window as any).api = api;

export { api };
export const ApiContext = createContext({
  poolsInfo: [] as Pool[],
  poolsInfoStatus: { pending: false },
});
