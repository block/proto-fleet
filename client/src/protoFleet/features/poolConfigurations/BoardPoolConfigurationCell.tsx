import { useSavedPoolAssignments } from "./assignmentContext";
import { usePoolAssignmentProgress } from "./assignmentProgress";
import { PoolStatus } from "./PoolStatus";
import { poolUpdateStatus } from "./status";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";

export const BoardPoolConfigurationCell = ({ moduleId, hashboardId }: { moduleId?: bigint; hashboardId?: bigint }) => {
  const saved = useSavedPoolAssignments();
  const accountId = useAccountScope()?.account.accountId ?? "";
  const job = usePoolAssignmentProgress((state) =>
    [...state.jobs]
      .reverse()
      .find(
        (job) =>
          job.accountId === accountId &&
          saved?.boards?.some(
            (board) =>
              job.installationIds.includes(String(board.installationId)) &&
              (moduleId !== undefined ? board.moduleId === moduleId : board.hashboardId === hashboardId),
          ),
      ),
  );
  if (!saved?.readable) return <span className="text-text-primary-50">Unavailable</span>;
  const rows =
    saved.boards?.filter((board) =>
      moduleId !== undefined ? board.moduleId === moduleId : board.hashboardId === hashboardId,
    ) ?? [];
  const local = job && (saved.loadedAt ?? 0) <= job.acceptedAt;
  const assigned = rows.filter((row) => row.configurationId);
  const statuses = rows.map(poolUpdateStatus);
  const same = statuses.every((status) => status === statuses[0]);
  const verified = rows.length > 0 && statuses.every((status) => status === "Verified");
  const mixed = rows.some((row) => row.configurationId !== rows[0]?.configurationId);
  const settled = statuses.every((status) =>
    ["Verified", "—", "Device configuration", "Container configuration"].includes(status),
  );
  const status = local
    ? job.phase === "unknown"
      ? "Unavailable"
      : "Updating…"
    : saved.error
      ? "Unavailable"
      : !rows.length
        ? saved.loading
          ? "Loading…"
          : "Unavailable"
        : mixed && settled
          ? "Mixed configurations"
          : verified
            ? "Verified"
            : same
              ? statuses[0]
              : statuses.every((status) => status === "Verified" || status === "Updating…")
                ? "Updating…"
                : "Needs attention";
  return (
    <PoolStatus
      status={status}
      name={assigned[0]?.name}
      onDetails={() =>
        usePoolAssignmentProgress.setState({
          detailsJobId: undefined,
          detailsSelection: {
            accountId,
            moduleIds: moduleId !== undefined ? [moduleId] : [],
            hashboardIds: hashboardId !== undefined ? [hashboardId] : [],
            jobId: local ? job.id : undefined,
          },
        })
      }
    />
  );
};
