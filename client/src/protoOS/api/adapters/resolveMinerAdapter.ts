import type { AdapterResolution, MinerAdapterDefinition } from "./types";

export type ProbeRequest = (path: string, signal: AbortSignal) => Promise<unknown>;

export const resolveMinerAdapter = async (
  definitions: readonly MinerAdapterDefinition[],
  request: ProbeRequest,
  signal: AbortSignal,
): Promise<AdapterResolution> => {
  for (const definition of definitions) {
    let body: unknown;
    try {
      body = await request(definition.probe.path, signal);
    } catch (error) {
      if (signal.aborted) throw error;
      continue;
    }

    const match = definition.probe.parseResponse(body);
    if (!match) continue;

    if (definition.support === "probe-only") {
      return { status: "unsupported", adapterId: definition.id, adapter: undefined };
    }

    return { status: "ready", adapterId: definition.id, adapter: definition.create(match) };
  }

  return { status: "unsupported", adapterId: undefined, adapter: undefined };
};
