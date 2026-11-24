import { isRecord, optionalString } from "../probeParsing";
import type { AdapterProbeMatch, MinerAdapter, MinerAdapterDefinition } from "../types";

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

const parseCt1ProbeResponse = (body: unknown): AdapterProbeMatch | undefined => {
  const match = parseMdkV2ProbeResponse(body);
  return match?.product?.toUpperCase() === "CT1" ? match : undefined;
};

export const createMdkV2Ct1Definition = (
  create: (match: AdapterProbeMatch) => MinerAdapter,
): MinerAdapterDefinition => ({
  id: "mdk-v2",
  displayName: "MDK v2 CT1",
  support: "supported",
  probe: {
    path: "/api/v2/system/info",
    parseResponse: parseCt1ProbeResponse,
  },
  create,
});

/**
 * Catch-all registration for MDK v2 products that do not have a supported
 * adapter yet. Keep this after the CT1 definition so another v2 product (for
 * example an R2) remains a deliberate unsupported result.
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
