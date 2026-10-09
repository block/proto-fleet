import { Code, ConnectError } from "@connectrpc/connect";
import { containersClient } from "@/protoFleet/api/clients";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { pushToast, STATUSES, updateToast } from "@/shared/features/toaster";

export async function runMiningAction(
  ids: bigint[],
  kind: "tank" | "module",
  mining: boolean,
  onRefresh: () => void,
  onActionComplete?: () => void,
) {
  const noun = ids.length === 1 ? kind : `${kind}s`;
  const toastId = pushToast({
    message: `${mining ? "Starting" : "Stopping"} mining on ${ids.length} ${noun}`,
    status: STATUSES.loading,
    longRunning: true,
  });
  try {
    const { commands } =
      kind === "tank"
        ? await containersClient.setTankMining({ tankIds: ids, mining }, { timeoutMs: 10_000 })
        : await containersClient.setModuleMining({ moduleIds: ids, mining }, { timeoutMs: 10_000 });
    if (!commands.length) throw new Error("Couldn't confirm whether the request was sent.");
    onActionComplete?.();

    let successCount = 0;
    let unconfirmed = false;
    const successMessage = () =>
      `${mining ? "Started" : "Stopped"} mining on ${successCount} out of ${ids.length} ${noun}`;

    await Promise.all(
      commands.map(async (command) => {
        const deadline = Date.now() + 120_000;
        try {
          while (Date.now() < deadline) {
            const { result } = await containersClient
              .getEquipmentCommandResult(
                { deviceIdentifier: command.deviceIdentifier, batchIdentifier: command.batchIdentifier },
                { timeoutMs: 10_000 },
              )
              .catch((cause: unknown) => {
                // Dispatch holds the ownership lock used by result reads. A
                // timed-out read can be retried without resending the action.
                if (ConnectError.from(cause).code === Code.DeadlineExceeded) return { result: undefined };
                throw cause;
              });
            // As with miner bulk actions, counts alone do not confirm completion.
            if (result?.status !== "finished") {
              await new Promise<void>((resolve) => setTimeout(resolve, 1000));
              continue;
            }
            const succeeded = result.successCount === 1 && result.failureCount === 0;
            // A failed controller batch can contain partial or unknown writes;
            // it cannot identify confirmed failures within that controller’s selection.
            if (result.detailsPruned || !succeeded) {
              unconfirmed = true;
              return;
            }
            successCount += "tankIds" in command ? command.tankIds.length : command.moduleIds.length;
            updateToast(toastId, { message: successMessage(), status: STATUSES.loading });
            return;
          }
          unconfirmed = true;
        } catch {
          unconfirmed = true;
        }
      }),
    );

    if (unconfirmed) {
      updateToast(toastId, {
        message:
          successCount > 0
            ? `${successMessage()}. Unable to confirm the remaining ${kind}s. Check ${kind} status before trying again.`
            : `Unable to confirm bulk action completion. Check ${kind} status before trying again.`,
        status: STATUSES.error,
      });
      return;
    }
    updateToast(toastId, { message: successMessage(), status: STATUSES.success });
  } catch (cause) {
    updateToast(toastId, { message: getErrorMessage(cause), status: STATUSES.error });
    onActionComplete?.();
  } finally {
    onRefresh();
  }
}
