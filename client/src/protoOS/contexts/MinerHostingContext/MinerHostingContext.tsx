import { createContext, ReactNode, useEffect, useMemo } from "react";
import { Api, RequestParams } from "@/protoOS/api/generatedApi";
import { useAuthTokens } from "@/protoOS/store/hooks/useAuth";

// The security data is the raw access token string.
type SecurityData = string;

const securityWorker = (token: SecurityData | null): RequestParams | void => {
  if (!token) return;
  return {
    headers: {
      Authorization: `Bearer ${token}`,
    },
  };
};

const CreateApi = (baseUrl: string) => {
  const url = (baseUrl.length ? "/" : "") + baseUrl;
  const instance = new Api<SecurityData>({
    baseUrl: url,
    securityWorker,
    // Require auth on all requests by default (matches miner-firmware PR #3266).
    // Callers of truly public endpoints (system/status, pairing/info) must pass
    // { secure: false } to opt out.
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
  const authTokens = useAuthTokens();

  // Keep the API client's security data in sync with the store's access token.
  useEffect(() => {
    instance.setSecurityData(authTokens.accessToken?.value || null);
  }, [instance, authTokens.accessToken?.value]);

  const api = instance.api;

  return (
    <MinerHostingContext.Provider value={{ api, minerRoot, closeButton }}>{children}</MinerHostingContext.Provider>
  );
};

export default MinerHostingContext;
