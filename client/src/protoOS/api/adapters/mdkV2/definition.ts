import { isRecord, optionalString } from "../probeParsing";
import type { AdapterProbeMatch, MinerAdapterDefinition } from "../types";

const parseMdkV2ProbeResponse = (body: unknown): AdapterProbeMatch | undefined => {
  if (!isRecord(body) || !isRecord(body.info)) return undefined;

  const reportedVersion = optionalString(body.info.version);
  if (!reportedVersion) return undefined;

  return {
    deviceId: optionalString(body.info.id),
    product: optionalString(body.info.product),
    reportedVersion,
  };
};

/**
 * Probe-only registration until the MDK v2 adapter is implemented. The probe
 * shape comes from MDK-v2-API.json's v2GetSystemInfoResponse/v2SystemInfo.
 */
export const mdkV2Definition: MinerAdapterDefinition = {
  id: "mdk-v2",
  displayName: "MDK v2",
  support: "probe-only",
  probe: {
    path: "/api/v2/system/info",
    parseResponse: parseMdkV2ProbeResponse,
  },
};
