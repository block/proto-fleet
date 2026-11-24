import { ETCD_UNHEALTHY, FRIGATE_UPDATE_MESSAGES_BY_CODE } from "@/shared/features/frigateUpdate/codeText";

export { ETCD_UNHEALTHY };

export const REMOTE_TRIGGER_DISABLED =
  "Software updates through the controller API are turned off for this container. Set update.allow_remote_trigger to true in the controller configuration, then check again.";

// Frigate's codes as Fleet shows them too, plus the ones whose next step is specific to this dialog.
const CODE_TEXT: Record<string, string> = {
  ...FRIGATE_UPDATE_MESSAGES_BY_CODE,
  REMOTE_TRIGGER_DISABLED,
  ACTIVE_JOB: "Another software update is already running on these controllers.",
  SELF_COORDINATION_UNSUPPORTED:
    "A controller's software version can't coordinate its own update. Update it once by hand to a version that supports it.",
  HELPER_UNAVAILABLE:
    "A controller's update helper isn't available. Make sure the controller's update helper service is running.",
  NOT_STAGED: `${FRIGATE_UPDATE_MESSAGES_BY_CODE.NOT_STAGED} Discard this update and choose the package again.`,
  STAGE_MISMATCH: `${FRIGATE_UPDATE_MESSAGES_BY_CODE.STAGE_MISMATCH} Discard this update and choose the package again.`,
};

export interface FrigateUpdateMessage {
  /** Mapped text, or the controller's message when the code is unknown. */
  title: string;
  /** The controller's own message, when the title is mapped text. */
  detail?: string;
  /** Kept visible so an unmapped failure can still be reported. */
  code?: string;
}

export function describeFrigateUpdateProblem(code: string, message: string): FrigateUpdateMessage {
  const text = code ? CODE_TEXT[code.replace(/^UPDATE_/, "")] : undefined;
  if (text) return { title: text, detail: message && message !== text ? message : undefined, code };
  return { title: message || "The controller refused the update.", code: code || undefined };
}
