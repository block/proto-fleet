import { createContext, ReactNode, useMemo } from "react";
import { Api, RequestParams } from "@/protoOS/api/generatedApi";
import useMinerStore from "@/protoOS/store/useMinerStore";
import type { MinerMetadata } from "@/shared/types/minerMetadata";

export type MinerHostingMode = "direct" | "fleet";

export type MinerHostingMetadata = MinerMetadata;

// Stable identity for the absent-metadata case so consumers' memoization
// (useMinerHosting) isn't busted by a fresh `{}` on every provider render.
const EMPTY_METADATA: MinerHostingMetadata = {};

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

const CreateApi = (baseUrl: string, mode: MinerHostingMode) => {
  const url = baseUrl.length ? (baseUrl.startsWith("/") ? baseUrl : `/${baseUrl}`) : "";
  const instance = new Api({
    baseUrl: url,
    securityWorker: mode === "direct" ? securityWorker : undefined,
    // Require auth on all requests by default; firmware now gates nearly every
    // endpoint. Callers of truly public endpoints (system/status, pairing/info)
    // must pass { secure: false } to opt out.
    baseApiParams: { secure: true },
  });

  // The standalone protoOS app and its E2E suite read the miner client off the
  // window (e.g. general.spec.ts verifies the system tag via window.api). Only
  // expose it in direct mode — never leak the fleet-proxied client globally.
  if (mode === "direct") {
    (window as unknown as { api: InstanceType<typeof Api>["api"] }).api = instance.api;
  }

  return instance;
};

type ApiT = InstanceType<typeof Api>["api"];

type MinerHostingContextType = {
  api: ApiT | null;
  minerRoot: string;
  closeButton: ReactNode | null;
  mode: MinerHostingMode;
  metadata: MinerHostingMetadata;
};

const MinerHostingContext = createContext<MinerHostingContextType>({
  api: null,
  minerRoot: "",
  closeButton: null,
  mode: "direct",
  metadata: EMPTY_METADATA,
});

type MinerHostingProviderProps = {
  children: ReactNode;
  baseUrl?: string;
  minerRoot?: string;
  closeButton?: ReactNode | null;
  mode?: MinerHostingMode;
  metadata?: MinerHostingMetadata;
};

export const MinerHostingProvider = ({
  children,
  baseUrl = "",
  minerRoot = "",
  closeButton = null,
  mode = "direct",
  metadata = EMPTY_METADATA,
}: MinerHostingProviderProps) => {
  const instance = useMemo(() => CreateApi(baseUrl, mode), [baseUrl, mode]);
  const api = instance.api;

  return (
    <MinerHostingContext.Provider value={{ api, minerRoot, closeButton, mode, metadata }}>
      {children}
    </MinerHostingContext.Provider>
  );
};

export default MinerHostingContext;
