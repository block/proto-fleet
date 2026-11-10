import { useSavedPoolAssignments } from "./assignmentContext";
import { findPoolAssignment, usePoolAssignmentProgress } from "./assignmentProgress";
import { PoolStatus } from "./PoolStatus";
import { poolUpdateStatus } from "./status";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";

export const PoolConfigurationCell = ({
  deviceIdentifier,
  deviceId,
}: {
  deviceIdentifier?: string;
  deviceId?: bigint;
}) => {
  const accountId = useAccountScope()?.account.accountId ?? "";
  const job = usePoolAssignmentProgress((state) =>
    findPoolAssignment(state.jobs, accountId, deviceIdentifier, deviceId),
  );
  const saved = useSavedPoolAssignments();
  const assignment =
    deviceId !== undefined ? saved?.byId.get(String(deviceId)) : saved?.byIdentifier.get(deviceIdentifier ?? "");
  const local =
    saved?.readable !== false &&
    job &&
    (!saved || (!saved.loadedAt && !saved.error) || (saved.loadedAt ?? 0) <= job.acceptedAt);
  const target = job?.targets.find(
    (item) =>
      item.kind === "device" &&
      (deviceId !== undefined ? item.targetId === deviceId : item.deviceIdentifier === deviceIdentifier),
  );
  const status = local
    ? job.phase === "unknown"
      ? "Unavailable"
      : target
        ? poolUpdateStatus(target)
        : "Updating…"
    : saved?.readable === false || saved?.error
      ? "Unavailable"
      : assignment
        ? poolUpdateStatus(assignment)
        : saved?.loading
          ? "Loading…"
          : "Unavailable";
  return (
    <PoolStatus
      status={status}
      name={local ? job.name : assignment?.name}
      onDetails={() =>
        usePoolAssignmentProgress.setState({
          detailsJobId: undefined,
          detailsSelection: {
            accountId,
            deviceIdentifiers: deviceIdentifier ? [deviceIdentifier] : [],
            deviceIds: deviceId !== undefined ? [deviceId] : [],
            jobId: local ? job.id : undefined,
          },
        })
      }
    />
  );
};
