import { Code } from "@connectrpc/connect";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

export type PowerFeedOutcome =
  { status: "pending" } | { status: "confirmed" } | { status: "failed"; message: string } | { status: "unknown" };

// Codes that prove the server queued nothing. Anything else may have queued
// the command, so its outcome is unknown and the operator must not resend.
export const notQueued = new Set([
  Code.InvalidArgument,
  Code.FailedPrecondition,
  Code.NotFound,
  Code.PermissionDenied,
  Code.Unauthenticated,
  Code.Unimplemented,
]);

// Texts for a write that may have run: an unconfirmed dispatch or poll, a
// command interrupted by a Fleet restart, or one reaped while processing.
const possiblyApplied = /\bunconfirmed\b|may be unknown|\breaped:/i;

/** A command the server proved it never queued: always failed, whatever its reason says. */
export function powerFeedRefusal(cause: unknown): PowerFeedOutcome {
  // Device results carry "<code>: <scope>: <reason>"; keep the reason.
  const reason = getErrorMessage(cause, "The command failed.")
    .replace(/^[a-z_]+: /, "")
    .replace(/^container power feed: /, "");
  return { status: "failed", message: reason.charAt(0).toUpperCase() + reason.slice(1) };
}

/** Classifies a tracked command failure; a write that may have run is unknown, not failed. */
export function powerFeedFailure(cause: unknown): PowerFeedOutcome {
  return possiblyApplied.test(getErrorMessage(cause, "The command failed."))
    ? { status: "unknown" }
    : powerFeedRefusal(cause);
}
