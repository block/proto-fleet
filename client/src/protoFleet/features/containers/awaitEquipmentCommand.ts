import { Code, ConnectError } from "@connectrpc/connect";
import { containersClient } from "@/protoFleet/api/clients";
import { createErrorWithCause } from "@/protoFleet/api/requestErrors";

const unconfirmed = "Command outcome unconfirmed. Refresh before retrying.";

// Observe one accepted command without ever resubmitting it. Return the full
// result so equipment controls can require telemetry newer than completion.
export async function awaitEquipmentCommand(
  deviceIdentifier: string,
  batchIdentifier: string,
  signal?: AbortSignal,
  deadline = Date.now() + 120_000,
) {
  if (signal?.aborted || Date.now() >= deadline) throw new Error(unconfirmed);
  const abort = new AbortController();
  const cancel = () => abort.abort();
  const timeout = setTimeout(cancel, Math.max(0, deadline - Date.now()));
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  const canceled = new Promise<never>((_, reject) => {
    abort.signal.addEventListener("abort", () => reject(new Error(unconfirmed)), { once: true });
  });
  signal?.addEventListener("abort", cancel, { once: true });
  try {
    while (true) {
      let result;
      try {
        if (abort.signal.aborted) throw new Error(unconfirmed);
        ({ result } = await Promise.race([
          containersClient.getEquipmentCommandResult(
            { deviceIdentifier, batchIdentifier },
            { signal: abort.signal, timeoutMs: 10_000 },
          ),
          canceled,
        ]));
      } catch (cause) {
        if (abort.signal.aborted || ConnectError.from(cause).code !== Code.DeadlineExceeded) {
          throw createErrorWithCause(unconfirmed, cause);
        }
      }
      if (result?.status === "finished") {
        if (result.failureCount > 0 || result.successCount !== 1 || result.detailsPruned) {
          throw new Error(
            result.deviceResults.find((device) => device.errorMessage)?.errorMessage ||
              "Equipment command failed; state unconfirmed.",
          );
        }
        return result;
      }
      await Promise.race([
        new Promise<void>((resolve) => {
          pollTimer = setTimeout(resolve, 1000);
        }),
        canceled,
      ]);
    }
  } finally {
    clearTimeout(timeout);
    clearTimeout(pollTimer);
    signal?.removeEventListener("abort", cancel);
  }
}
