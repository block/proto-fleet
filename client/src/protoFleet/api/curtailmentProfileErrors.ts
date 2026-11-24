import { Code, ConnectError } from "@connectrpc/connect";

export const CURTAILMENT_PROFILES_STALE_EVENT = "protoFleet:curtailment-profiles-stale";
export const responseProfileExecutionConflictMessage =
  "This profile changed. Reload the profile and review the updated plan before running curtailment.";
export function isResponseProfileExecutionConflict(error: unknown): boolean {
  return (
    error instanceof ConnectError &&
    error.code === Code.FailedPrecondition &&
    (error.rawMessage === "curtailment response profile changed before execution; reload and retry" ||
      error.rawMessage === "curtailment response profile values do not match the expected revision; reload and retry")
  );
}
export function notifyCurtailmentProfilesStale(): void {
  window.dispatchEvent(new Event(CURTAILMENT_PROFILES_STALE_EVENT));
}
