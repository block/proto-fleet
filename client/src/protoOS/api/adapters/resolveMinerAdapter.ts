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
  const responses = new Map<string, Promise<unknown>>();
  const failedSupportedProbePaths = new Set<string>();

  for (const definition of definitions) {
    try {
      // Keep request, parseResponse, and create in one try: a parse or
      // construct throw is a probe failure, not a well-formed non-match.
      let response = responses.get(definition.probe.path);
      if (!response) {
        response = request(definition.probe.path, signal);
        responses.set(definition.probe.path, response);
      }
      const body = await response;
      const match = definition.probe.parseResponse(body);
      if (!match) continue;

      if (definition.support === "probe-only") {
        // A catch-all definition that shares a response with a supported
        // definition must not turn that definition's parse/create failure into
        // a terminal unsupported result. Continue probing other API versions;
        // if none match, the failed supported probe resolves to unreachable.
        if (failedSupportedProbePaths.has(definition.probe.path)) continue;
        return { status: "unsupported", adapterId: definition.id, adapter: undefined };
      }

      return { status: "ready", adapterId: definition.id, adapter: definition.create(match) };
    } catch (error) {
      if (signal.aborted) throw error;
      probeErrored = true;
      if (definition.support === "supported") failedSupportedProbePaths.add(definition.probe.path);
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
