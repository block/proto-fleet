import { createContext, ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { useResetDeviceStateOnMinerChange } from "./useResetDeviceStateOnMinerChange";
import { Api, RequestParams } from "@/protoOS/api/adapters/mdkV1";
import { createMdkV1Definition } from "@/protoOS/api/adapters/mdkV1/definition";
import { MdkV1Adapter } from "@/protoOS/api/adapters/mdkV1/MdkV1Adapter";
// TODO: Make this a factory when MDK v2 has an adapter that needs the active transport.
import { mdkV2Definition } from "@/protoOS/api/adapters/mdkV2/definition";
import { resolveMinerAdapter } from "@/protoOS/api/adapters/resolveMinerAdapter";
import type { AdapterResolution, MinerAdapter } from "@/protoOS/api/adapters/types";
import UnsupportedMiner from "@/protoOS/components/UnsupportedMiner";
import { authTokensFromSlice } from "@/protoOS/store/slices/authSlice";
import useMinerStore from "@/protoOS/store/useMinerStore";
import type { MinerMetadata } from "@/shared/types/minerMetadata";

export type MinerHostingMode = "direct" | "fleet";

export type MinerHostingMetadata = MinerMetadata;

// Stable identity for the absent-metadata case so consumers' memoization
// (useMinerHosting) isn't busted by a fresh `{}` on every provider render.
const EMPTY_METADATA: MinerHostingMetadata = {};

// The bearer the v1 adapter decorates its own requests with. Read at request
// time so every call picks up the latest value from the store (login/refresh
// write here). Only wired in direct mode — in fleet mode the minerproxy owns
// auth, so the adapter gets no credential and decorates nothing.
const directAccessToken = () => authTokensFromSlice(useMinerStore.getState().auth).accessToken.value;

// Authenticates the raw client exposed on `window.api` below. The app itself
// authenticates through the adapter's decorate(); this securityWorker exists
// only so the standalone/E2E window handle — which calls generated methods
// directly, with no adapter in front of it — still carries a bearer on its
// @secure calls (e.g. deleteSystemTag). Wired in direct mode only, where the
// window handle is exposed; in fleet mode the store holds no token and the
// minerproxy owns auth, so it never fires.
const directBearerSecurityWorker = (): RequestParams | void => {
  const token = directAccessToken();
  if (!token) return;
  return { headers: { Authorization: `Bearer ${token}` } };
};

const CreateApi = (baseUrl: string, mode: MinerHostingMode) => {
  const url = baseUrl.length ? (baseUrl.startsWith("/") ? baseUrl : `/${baseUrl}`) : "";
  const instance = new Api({
    baseUrl: url,
    securityWorker: mode === "direct" ? directBearerSecurityWorker : undefined,
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

const UNREACHABLE_RESOLUTION: AdapterResolution = {
  status: "unreachable",
  adapterId: undefined,
  adapter: undefined,
};

// Bound each probe request so a hung/never-responding miner can't wedge adapter
// resolution forever. A timed-out attempt aborts only that request (not the
// effect's parent signal), so resolveMinerAdapter treats it as a normal probe
// error and resolves to "unreachable" — a retryable state, not a permanent hang.
const PROBE_TIMEOUT_MS = 5000;

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
  // The provider carries the credential to the adapter: direct mode hands v1 the
  // store bearer to decorate with; fleet mode hands none (proxy owns auth).
  const credential = mode === "direct" ? directAccessToken : undefined;
  const definitions = useMemo(
    () => [mdkV2Definition, createMdkV1Definition(() => new MdkV1Adapter(api, credential))],
    [api, credential],
  );
  const [resolved, setResolved] = useState<{ instance: typeof instance; resolution: AdapterResolution }>(() => ({
    instance,
    resolution: PROBING_RESOLUTION,
  }));
  // Bumped to force a re-probe (e.g. the retry button on the unreachable state).
  const [retryToken, setRetryToken] = useState(0);
  // Only surface a resolution that belongs to the *current* api instance. When
  // baseUrl/mode change, `instance` is recreated before the new probe settles;
  // discarding the previous instance's still-pending resolution here keeps us in
  // `probing` instead of flashing the prior miner's adapter (a stale-adapter
  // flash) during the switch.
  const resolution = resolved.instance === instance ? resolved.resolution : PROBING_RESOLUTION;
  const adapter = resolution.status === "ready" ? resolution.adapter : null;

  const retry = useCallback(() => {
    // Show probing immediately, then re-run the effect via the deps bump.
    setResolved({ instance, resolution: PROBING_RESOLUTION });
    setRetryToken((token) => token + 1);
  }, [instance]);

  useEffect(() => {
    const controller = new AbortController();
    const probe = async (path: string, signal: AbortSignal) => {
      // Race the request against a per-attempt timeout: a child controller
      // aborts on timeout OR when the effect's parent signal aborts. The parent
      // signal itself is left untouched by the timeout, so a timeout reads as a
      // normal probe error (→ unreachable) while a genuine abort still rethrows.
      const attempt = new AbortController();
      const forwardAbort = () => attempt.abort();
      if (signal.aborted) {
        attempt.abort();
      } else {
        signal.addEventListener("abort", forwardAbort, { once: true });
      }
      const timeout = setTimeout(forwardAbort, PROBE_TIMEOUT_MS);
      try {
        const response = await instance.request<unknown, unknown>({
          path,
          method: "GET",
          format: "json",
          signal: attempt.signal,
        });
        return response.data;
      } finally {
        clearTimeout(timeout);
        signal.removeEventListener("abort", forwardAbort);
      }
    };

    void resolveMinerAdapter(definitions, probe, controller.signal)
      .then((nextResolution) => {
        if (!controller.signal.aborted) setResolved({ instance, resolution: nextResolution });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        console.error("Miner adapter resolution failed", error);
        setResolved({ instance, resolution: UNREACHABLE_RESOLUTION });
      });

    return () => controller.abort();
  }, [definitions, instance, retryToken]);

  // baseUrl is per-miner in fleet mode, so this clears the singleton device
  // store when switching miners (no-op in direct mode, where baseUrl is fixed).
  useResetDeviceStateOnMinerChange(baseUrl);

  return (
    <MinerHostingContext.Provider value={{ api, adapter, resolution, minerRoot, closeButton, mode, metadata }}>
      {resolution.status === "unsupported" ? (
        <UnsupportedMiner adapterId={resolution.adapterId} closeButton={closeButton} />
      ) : resolution.status === "unreachable" ? (
        <UnsupportedMiner closeButton={closeButton} onRetry={retry} />
      ) : (
        children
      )}
    </MinerHostingContext.Provider>
  );
};

export default MinerHostingContext;
