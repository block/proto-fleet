import { createContext, ReactNode, useEffect, useMemo, useState } from "react";
import { useResetDeviceStateOnMinerChange } from "./useResetDeviceStateOnMinerChange";
import { createMdkV1Definition } from "@/protoOS/api/adapters/mdkV1/definition";
import { MdkV1Adapter } from "@/protoOS/api/adapters/mdkV1/MdkV1Adapter";
// TODO: Make this a factory when MDK v2 has an adapter that needs the active transport.
import { mdkV2Definition } from "@/protoOS/api/adapters/mdkV2/definition";
import { resolveMinerAdapter } from "@/protoOS/api/adapters/resolveMinerAdapter";
import type { AdapterResolution, MinerAdapter } from "@/protoOS/api/adapters/types";
import { Api, RequestParams } from "@/protoOS/api/generatedApi";
import UnsupportedMiner from "@/protoOS/components/UnsupportedMiner";
import { authTokensFromSlice } from "@/protoOS/store/slices/authSlice";
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
  const token = authTokensFromSlice(useMinerStore.getState().auth).accessToken.value;
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
  adapter: MinerAdapter | null;
  resolution: AdapterResolution;
  minerRoot: string;
  closeButton: ReactNode | null;
  mode: MinerHostingMode;
  metadata: MinerHostingMetadata;
};

const PROBING_RESOLUTION: AdapterResolution = {
  status: "probing",
  adapterId: undefined,
  adapter: undefined,
};

const MinerHostingContext = createContext<MinerHostingContextType>({
  api: null,
  adapter: null,
  resolution: PROBING_RESOLUTION,
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
  const definitions = useMemo(() => [mdkV2Definition, createMdkV1Definition(() => new MdkV1Adapter(api))], [api]);
  const [resolved, setResolved] = useState<{ instance: typeof instance; resolution: AdapterResolution }>(() => ({
    instance,
    resolution: PROBING_RESOLUTION,
  }));
  const resolution = resolved.instance === instance ? resolved.resolution : PROBING_RESOLUTION;
  const adapter = resolution.status === "ready" ? resolution.adapter : null;

  useEffect(() => {
    const controller = new AbortController();
    const probe = async (path: string, signal: AbortSignal) => {
      const response = await instance.request<unknown, unknown>({
        path,
        method: "GET",
        format: "json",
        secure: false,
        signal,
      });
      return response.data;
    };

    void resolveMinerAdapter(definitions, probe, controller.signal)
      .then((nextResolution) => {
        if (!controller.signal.aborted) setResolved({ instance, resolution: nextResolution });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        console.error("Miner adapter resolution failed", error);
        setResolved({
          instance,
          resolution: { status: "unsupported", adapterId: undefined, adapter: undefined },
        });
      });

    return () => controller.abort();
  }, [definitions, instance]);

  // baseUrl is per-miner in fleet mode, so this clears the singleton device
  // store when switching miners (no-op in direct mode, where baseUrl is fixed).
  useResetDeviceStateOnMinerChange(baseUrl);

  return (
    <MinerHostingContext.Provider value={{ api, adapter, resolution, minerRoot, closeButton, mode, metadata }}>
      {resolution.status === "unsupported" ? (
        <UnsupportedMiner adapterId={resolution.adapterId} closeButton={closeButton} />
      ) : (
        children
      )}
    </MinerHostingContext.Provider>
  );
};

export default MinerHostingContext;
