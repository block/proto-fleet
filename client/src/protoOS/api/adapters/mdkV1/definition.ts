import { isRecord, optionalString } from "../probeParsing";
import type { AdapterProbeMatch, MinerAdapter, MinerAdapterDefinition } from "../types";

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
