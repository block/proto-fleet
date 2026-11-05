import { useCallback } from "react";
import { usePoolConfigurationClient } from "./api";
import { usePoolAssignmentProgress } from "./assignmentProgress";
import { ConfirmationRecovery } from "./ConfirmationRecovery";
import { OperationResults } from "./OperationResults";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";

// Keep details outside table rows: telemetry refreshes can temporarily unmount
// them. Reload assignments here so an open dialog also sees newer observations.
export const PoolAssignmentFeedback = () => {
  const { client, accountId } = usePoolConfigurationClient();
  const selection = usePoolAssignmentProgress((state) =>
    state.detailsSelection?.accountId === accountId ? state.detailsSelection : undefined,
  );
  const job = usePoolAssignmentProgress((state) =>
    state.jobs.find((job) => job.id === (selection?.jobId ?? state.detailsJobId) && job.accountId === accountId),
  );
  const load = useCallback(
    (signal: AbortSignal) =>
      client.getAssignments(
        {
          accountId,
          deviceIdentifiers: selection?.deviceIdentifiers,
          deviceIds: selection?.deviceIds,
          boards:
            selection?.moduleIds || selection?.hashboardIds
              ? {
                  moduleIds: selection.moduleIds,
                  hashboardIds: selection.hashboardIds,
                }
              : undefined,
        },
        { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
      ),
    [client, accountId, selection],
  );
  const query = useAccountQuery(load, !!selection, true, 3000);

  const assignments = query.data?.assignments ?? [];
  const activeJob = !selection || !assignments.length ? job : undefined;
  return (
    <>
      <ConfirmationRecovery />
      {job || selection ? (
        <OperationResults
          key={
            selection
              ? `${accountId}:${selection.deviceIdentifiers}:${selection.deviceIds}:${selection.moduleIds}:${selection.hashboardIds}:${selection.jobId}`
              : job?.id
          }
          operationId={activeJob?.operationId}
          assignments={assignments}
          selectedTargets={
            selection
              ? assignments.map((a) => ({
                  kind: a.installationId ? "hashboard_installation" : "device",
                  targetId: a.installationId || a.deviceId,
                }))
              : undefined
          }
          loading={query.loading}
          unavailable={!!query.error}
          onRefresh={query.refresh}
          onOperationChange={query.refresh}
          onDismiss={() => usePoolAssignmentProgress.setState({ detailsJobId: undefined, detailsSelection: undefined })}
        />
      ) : null}
    </>
  );
};
