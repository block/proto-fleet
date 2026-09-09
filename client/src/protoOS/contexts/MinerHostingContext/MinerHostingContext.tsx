import { createContext, ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { useResetDeviceStateOnMinerChange } from "./useResetDeviceStateOnMinerChange";
import { createMdkV1Definition } from "@/protoOS/api/adapters/mdkV1/definition";
import { MdkV1Adapter } from "@/protoOS/api/adapters/mdkV1/MdkV1Adapter";
import { createMdkV2Ct1Definition, mdkV2Definition } from "@/protoOS/api/adapters/mdkV2/definition";
import { MdkV2Ct1Adapter } from "@/protoOS/api/adapters/mdkV2/MdkV2Ct1Adapter";
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

const normalizedBaseUrl = (baseUrl: string) =>
  baseUrl.length ? (baseUrl.startsWith("/") ? baseUrl : `/${baseUrl}`) : "";

type MinerHostingContextType = {
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
  // The normalized base is the miner's transport identity: each adapter builds
  // its own generated client for this URL, and the probe fetches against it.
  const url = normalizedBaseUrl(baseUrl);
  // The provider carries the credential to the adapter: direct mode hands v1 the
  // store bearer to decorate with; fleet mode hands none (proxy owns auth).
  const credential = mode === "direct" ? directAccessToken : undefined;
  // Each definition owns its transport construction (adapter.fromConfig builds
  // the generation-specific client for this URL), so the context imports no
  // generated runtime — only the adapter's dialect knows the wire.
  const definitions = useMemo(
    () => [
      createMdkV2Ct1Definition((match) => MdkV2Ct1Adapter.fromConfig({ baseUrl: url }, match)),
      mdkV2Definition,
      createMdkV1Definition(() => MdkV1Adapter.fromConfig({ baseUrl: url, getAccessToken: credential })),
    ],
    [url, credential],
  );
  const [resolved, setResolved] = useState<{ url: string; mode: MinerHostingMode; resolution: AdapterResolution }>(
    () => ({ url, mode, resolution: PROBING_RESOLUTION }),
  );
  // Bumped to force a re-probe (e.g. the retry button on the unreachable state).
  const [retryToken, setRetryToken] = useState(0);
  // Only surface a resolution that belongs to the *current* miner URL and hosting
  // mode. When either changes, a fresh probe is kicked off before it settles;
  // discarding the previous identity's still-pending resolution keeps us in
  // `probing` instead of flashing the prior miner's adapter (a stale-adapter
  // flash). Mode is part of the key because it selects the credential the adapter
  // is built with, so a mode flip must not surface an adapter carrying the wrong one.
  const resolution = resolved.url === url && resolved.mode === mode ? resolved.resolution : PROBING_RESOLUTION;
  const adapter = resolution.status === "ready" ? resolution.adapter : null;

  const retry = useCallback(() => {
    // Show probing immediately, then re-run the effect via the deps bump.
    setResolved({ url, mode, resolution: PROBING_RESOLUTION });
    setRetryToken((token) => token + 1);
  }, [url, mode]);

  useEffect(() => {
    const controller = new AbortController();
    // Probe with plain fetch, not a generation-specific client: identification
    // must not depend on any one adapter's wire (probing v2 with the v1 client
    // was the coupling this removes). An adapter builds its own client only once
    // its probe matches.
    const probe = async (path: string, signal: AbortSignal): Promise<unknown> => {
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
        const response = await fetch(`${url}${path}`, {
          method: "GET",
          signal: attempt.signal,
          // Match the generated client, which set referrerPolicy: "no-referrer".
          referrerPolicy: "no-referrer",
        });
        // Only a non-2xx is a probe error (→ recoverable "unreachable"). A 2xx
        // body that fails to parse is NOT an error: the generated v1 client
        // swallowed JSON-parse failures on ok responses and yielded a null body,
        // which resolveMinerAdapter reads as a well-formed non-match. Preserve
        // that so a non-miner server answering 200 with HTML on every probe path
        // lands on the terminal "unsupported" page instead of looping on retry.
        if (!response.ok) throw new Error(`Probe ${path} failed with status ${response.status}`);
        return await response.json().catch(() => null);
      } finally {
        clearTimeout(timeout);
        signal.removeEventListener("abort", forwardAbort);
      }
    };

    void resolveMinerAdapter(definitions, probe, controller.signal)
      .then((nextResolution) => {
        if (!controller.signal.aborted) setResolved({ url, mode, resolution: nextResolution });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        console.error("Miner adapter resolution failed", error);
        setResolved({ url, mode, resolution: UNREACHABLE_RESOLUTION });
      });

    return () => controller.abort();
  }, [definitions, url, mode, retryToken]);

  // baseUrl is per-miner in fleet mode, so this clears the singleton device
  // store when switching miners (no-op in direct mode, where baseUrl is fixed).
  useResetDeviceStateOnMinerChange(baseUrl);

  return (
    <MinerHostingContext.Provider value={{ adapter, resolution, minerRoot, closeButton, mode, metadata }}>
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
