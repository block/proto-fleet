import type { AdapterResolution, MinerAdapterDefinition } from "./types";

export type ProbeRequest = (path: string, signal: AbortSignal) => Promise<unknown>;

export const resolveMinerAdapter = async (
  definitions: readonly MinerAdapterDefinition[],
  request: ProbeRequest,
  signal: AbortSignal,
): Promise<AdapterResolution> => {
  // Whether any definition failed with a non-abort error (network reject, 5xx,
  // parse/construct throw). If so, and nothing matched, identification is
  // inconclusive ("unreachable") rather than a confirmed "unsupported" miner —
  // a transient failure may have masked a match, so the caller can re-probe.
  let probeErrored = false;

  for (const definition of definitions) {
    try {
      // Keep request, parseResponse, and create in one try: a parse or
      // construct throw is a probe failure, not a well-formed non-match.
      const body = await request(definition.probe.path, signal);
      const match = definition.probe.parseResponse(body);
      if (!match) continue;

      if (definition.support === "probe-only") {
        return { status: "unsupported", adapterId: definition.id, adapter: undefined };
      }

      return { status: "ready", adapterId: definition.id, adapter: definition.create(match) };
    } catch (error) {
      if (signal.aborted) throw error;
      probeErrored = true;
      continue;
    }
  }

  // Only a genuine "every probe returned a well-formed non-matching body" run is
  // terminally unsupported; a run where any probe errored is recoverable.
  if (probeErrored) {
    return { status: "unreachable", adapterId: undefined, adapter: undefined };
  }

  return { status: "unsupported", adapterId: undefined, adapter: undefined };
};
