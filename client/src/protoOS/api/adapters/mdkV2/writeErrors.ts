import { Code } from "@connectrpc/connect";

// The only codes the controller returns for container-control requests it rejects
// before writing (not primary, unsupported backend, bad request). Any other failure
// may have reached the PLC, so its outcome is unknown until readback.
const CONTAINER_REFUSAL_CODES: ReadonlySet<number> = new Set<number>([
  Code.InvalidArgument,
  Code.FailedPrecondition,
  Code.Unimplemented,
]);

/**
 * Classifies a failed controller write: a refusal changed nothing, and anything else leaves the
 * outcome unknown until readback. Authentication failures pass through for the caller's retry.
 */
export const containerWriteError = (
  error: unknown,
  unchanged: string,
  outcomeUnknown: string,
  refusalCodes: ReadonlySet<number> = CONTAINER_REFUSAL_CODES,
) => {
  const failure = error as { status?: number; error?: { code?: number; message?: string } } | undefined;
  // Leave authentication failures for the caller's token refresh and retry.
  if (failure?.status === 401 || failure?.error?.code === Code.Unauthenticated) return error;
  const code = failure?.error?.code;
  if (code !== undefined && refusalCodes.has(code)) {
    const detail = failure?.error?.message?.trim();
    return Object.assign(new Error(`The controller refused the request${detail ? `: ${detail}` : ""}. ${unchanged}`), {
      cause: error,
      writeOutcome: "refused" as const,
    });
  }
  return Object.assign(new Error(outcomeUnknown), { cause: error, writeOutcome: "unknown" as const });
};
