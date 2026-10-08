import { containersClient } from "@/protoFleet/api/clients";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { pushToast, removeToast, STATUSES, updateToast } from "@/shared/features/toaster";

export async function runTankMiningAction(
  tankIds: bigint[],
  mining: boolean,
  onRefresh: () => void,
  onActionComplete?: () => void,
) {
  const tankNoun = tankIds.length === 1 ? "tank" : "tanks";
  const toastId = pushToast({
    message: `${mining ? "Starting" : "Stopping"} mining on ${tankIds.length} ${tankNoun}`,
    status: STATUSES.loading,
    longRunning: true,
    onClose: onActionComplete,
  });
  try {
    const { commands } = await containersClient.setTankMining({ tankIds, mining }, { timeoutMs: 10_000 });
    if (!commands.length) throw new Error("Couldn't confirm whether the request was sent.");
    onActionComplete?.();

    let successCount = 0;
    const failedTankIds: bigint[] = [];
    let errorToastId: number | undefined;
    let unconfirmed = false;
    const successMessage = () =>
      `${mining ? "Started" : "Stopped"} mining on ${successCount} out of ${tankIds.length} ${tankNoun}`;

    await Promise.all(
      commands.map(async (command) => {
        const deadline = Date.now() + 120_000;
        try {
          while (Date.now() < deadline) {
            const { result } = await containersClient.getEquipmentCommandResult(
              { deviceIdentifier: command.deviceIdentifier, batchIdentifier: command.batchIdentifier },
              { timeoutMs: 10_000 },
            );
            // As with miner bulk actions, counts alone do not confirm completion.
            if (result?.status !== "finished") {
              await new Promise<void>((resolve) => setTimeout(resolve, 1000));
              continue;
            }
            const succeeded = result.successCount === 1 && result.failureCount === 0;
            const failed = result.failureCount === 1 && result.successCount === 0;
            if (result.detailsPruned || (!succeeded && !failed)) {
              unconfirmed = true;
              return;
            }
            if (succeeded) {
              successCount += command.tankIds.length;
              updateToast(toastId, { message: successMessage(), status: STATUSES.loading });
              return;
            }
            failedTankIds.push(...command.tankIds);
            const message = `Couldn't ${mining ? "start" : "stop"} mining on ${failedTankIds.length} out of ${tankIds.length} ${tankNoun}`;
            if (errorToastId === undefined) {
              errorToastId = pushToast({ message, status: STATUSES.error, longRunning: true });
            } else {
              updateToast(errorToastId, { message, status: STATUSES.error });
            }
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
        message: "Unable to confirm bulk action completion. Check tank status and try again.",
        status: STATUSES.error,
      });
      return;
    }
    if (successCount > 0) {
      updateToast(toastId, { message: successMessage(), status: STATUSES.success });
    } else {
      removeToast(toastId);
    }
    if (errorToastId === undefined) return;
    const retryToastId = errorToastId;
    const retryTankIds = [...failedTankIds];
    let retried = false;
    updateToast(retryToastId, {
      actions: [
        {
          label: "Retry",
          onClick: () => {
            if (retried) return;
            retried = true;
            removeToast(retryToastId);
            void runTankMiningAction(retryTankIds, mining, onRefresh, onActionComplete);
          },
        },
      ],
    });
  } catch (cause) {
    updateToast(toastId, { message: getErrorMessage(cause), status: STATUSES.error });
    onActionComplete?.();
  } finally {
    onRefresh();
  }
}
