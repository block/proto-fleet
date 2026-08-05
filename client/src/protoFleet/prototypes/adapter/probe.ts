/**
 * Version probe — the seam that lets one client pick the right adapter for a
 * given device. Mirrors the fake rig's public `GET /api/version`.
 */
import type { SingleMinerAdapter } from "../shared/adapter";
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
export async function probeAndResolve(baseUrl: string, password: string, signal?: AbortSignal): Promise<ProbeResult> {
  let version: VersionResponse | undefined;
  try {
    version = await getJson<VersionResponse>(`${baseUrl}/api/version`, { signal });
  } catch {
    // Pre-probe firmware — assume the legacy v1 REST surface.
    return { adapter: new MdkV1Adapter(baseUrl, password), mdkVersion: "1", firmwareRev: "unknown" };
  }

  const speaksV2 = version.apiVersions.includes("v2") || version.mdkVersion === "2";
  const adapter = speaksV2 ? new MdkV2Adapter(baseUrl) : new MdkV1Adapter(baseUrl, password);
  return { adapter, mdkVersion: version.mdkVersion, firmwareRev: version.firmwareRev };
}

export { hostOf };
