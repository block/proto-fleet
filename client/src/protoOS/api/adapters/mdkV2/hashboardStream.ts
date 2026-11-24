import type { AdapterRequestOptions, HashboardStatus } from "../types";
import type { MdkV2Client } from "./connectClient";
import type { V2HashboardASICLayout, V2StreamASICHealthResponse } from "./generated/generatedApi";
import { toCanonicalHashboardStatus } from "./normalize";

type FollowAsicHealth = NonNullable<MdkV2Client["telemetryServiceFollowAsicHealth"]>;

// CT1 frames carry the board MAC and R2 frames its serial; either may be empty.
const reportedIdentities = (frame: V2StreamASICHealthResponse) =>
  [frame.macAddress, frame.serial].filter((value): value is string => !!value).map((value) => value.toLowerCase());

/**
 * Follows StreamASICHealth for one board. The controller sends the ASIC layout
 * only on the first frame and when it changes, so each status reuses the last
 * layout it received.
 */
export async function* followHashboardStatus(
  follow: FollowAsicHealth,
  identifier: string,
  params: AdapterRequestOptions,
): AsyncGenerator<HashboardStatus> {
  let layout: V2HashboardASICLayout | undefined;
  for await (const frame of follow({ identifier }, params)) {
    const reported = reportedIdentities(frame);
    if (reported.length && !reported.includes(identifier.toLowerCase())) {
      throw new Error(`The controller streamed readings for ${reported[0]} instead of ${identifier}`);
    }
    layout = frame.layout ?? layout;
    const status = toCanonicalHashboardStatus(identifier, { asics: frame.asics, layout });
    yield frame.timestamp ? { ...status, observedAt: frame.timestamp } : status;
  }
}
