import type { AdapterProbeMatch, MinerAdapter, MinerAdapterDefinition } from "../types";

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const optionalString = (value: unknown): string | undefined =>
  typeof value === "string" && value.length > 0 ? value : undefined;

const parseMdkV1ProbeResponse = (body: unknown): AdapterProbeMatch | undefined => {
  if (!isRecord(body) || !isRecord(body["system-info"])) return undefined;

  const info = body["system-info"];
  return {
    deviceId: optionalString(info.cb_sn),
    product: optionalString(info.product_name),
  };
};

export const createMdkV1Definition = (create: () => MinerAdapter): MinerAdapterDefinition => ({
  id: "mdk-v1",
  displayName: "MDK v1",
  support: "supported",
  probe: {
    path: "/api/v1/system",
    parseResponse: parseMdkV1ProbeResponse,
  },
  create,
});
