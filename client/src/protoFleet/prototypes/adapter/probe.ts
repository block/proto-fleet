/**
 * Version probe — the seam that lets one client pick the right adapter for a
 * given device. Mirrors the fake rig's public `GET /api/version`.
 */
import type { SingleMinerAdapter } from "../shared/adapter";
import { type FlowTracer, NO_TRACE } from "../shared/flowTrace";
import { getJson, hostOf } from "./http";
import { MdkV1Adapter } from "./mdkV1Adapter";
import { MdkV2Adapter } from "./mdkV2Adapter";

interface VersionResponse {
  mdkVersion: string;
  apiVersions: string[];
  firmwareRev: string;
}

export interface ProbeResult {
  adapter: SingleMinerAdapter;
  mdkVersion: string;
  firmwareRev: string;
}

/**
 * Ask the device what it speaks, then resolve the matching adapter. Falls back
 * to MDK v1 for older firmware that predates the `/api/version` probe.
 */
export async function probeAndResolve(
  baseUrl: string,
  password: string,
  signal?: AbortSignal,
  tracer: FlowTracer = NO_TRACE,
): Promise<ProbeResult> {
  const req = tracer.request("miner", "GET /api/version", "probe firmware generation");
  let version: VersionResponse | undefined;
  try {
    version = await getJson<VersionResponse>(`${baseUrl}/api/version`, { signal });
    req.ok(`MDK v${version.mdkVersion} · fw ${version.firmwareRev}`);
  } catch {
    // Pre-probe firmware — assume the legacy v1 REST surface.
    req.ok("no probe → legacy v1");
    tracer.seam("Version seam → MDK v1", "legacy fallback — no /api/version probe");
    return { adapter: new MdkV1Adapter(baseUrl, password), mdkVersion: "1", firmwareRev: "unknown" };
  }

  const speaksV2 = version.apiVersions.includes("v2") || version.mdkVersion === "2";
  const adapter = speaksV2 ? new MdkV2Adapter(baseUrl) : new MdkV1Adapter(baseUrl, password);
  // The version seam: the probed firmware decides which client/adapter renders.
  tracer.seam(
    `Version seam → MDK v${speaksV2 ? "2" : "1"}`,
    "probed firmware decides which single-miner client to render",
  );
  return { adapter, mdkVersion: version.mdkVersion, firmwareRev: version.firmwareRev };
}

export { hostOf };
