import { createContext, ReactNode, useMemo } from "react";
import { Api, RequestParams } from "@/protoOS/api/generatedApi";
import useMinerStore from "@/protoOS/store/useMinerStore";

// Read the access token at request time so every call picks up the latest
// value from the store. Using setSecurityData from a useEffect races against
// child useEffects (which fire first on initial mount), so the first fetch
// would otherwise go out before the provider had a chance to inject auth.
const securityWorker = (): RequestParams | void => {
  const token = useMinerStore.getState().auth.authTokens.accessToken?.value;
  if (!token) return;
  return {
    headers: {
      Authorization: `Bearer ${token}`,
    },
  };
};

const CreateApi = (baseUrl: string) => {
  const url = (baseUrl.length ? "/" : "") + baseUrl;
  const instance = new Api({
    baseUrl: url,
    securityWorker,
    // Require auth on all requests by default; firmware now gates nearly every
    // endpoint. Callers of truly public endpoints (system/status, pairing/info)
    // must pass { secure: false } to opt out.
    baseApiParams: { secure: true },
  });

  // TODO: remove this when done with development
  (window as any).api = instance.api;

  return instance;
};

type ApiT = InstanceType<typeof Api>["api"];

type MinerHostingContextType = {
  api: ApiT | null;
  minerRoot: string;
  closeButton: ReactNode | null;
};

const MinerHostingContext = createContext<MinerHostingContextType>({
  api: null,
  minerRoot: "",
  closeButton: null,
});

type MinerHostingProviderProps = {
  children: ReactNode;
  baseUrl?: string;
  minerRoot?: string;
  closeButton?: ReactNode | null;
};

export const MinerHostingProvider = ({
  children,
  baseUrl = "",
  minerRoot = "",
  closeButton = null,
}: MinerHostingProviderProps) => {
  const instance = useMemo(() => CreateApi(baseUrl), [baseUrl]);
  const api = instance.api;

  return (
    <MinerHostingContext.Provider value={{ api, minerRoot, closeButton }}>{children}</MinerHostingContext.Provider>
  );
};

export default MinerHostingContext;
