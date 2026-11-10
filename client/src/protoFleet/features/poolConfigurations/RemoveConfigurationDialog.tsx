import { useCallback, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { usePoolConfigurationClient } from "./api";
import { pollPoolOperation, refreshPoolAssignments } from "./assignmentProgress";
import { poolRemovalOutcome } from "./status";
import type {
  EquipmentAssignment,
  GetOperationResponse,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import { Alert, Success } from "@/shared/assets/icons";
import Checkbox from "@/shared/components/Checkbox";
import ProgressCircular from "@/shared/components/ProgressCircular";

type PoolRemoval = {
  reviewId: string;
  operationId?: string;
  target: { kind: string; targetId: bigint };
  verificationState: string;
  clearSupported: boolean;
  state: string;
  resultCode?: string;
  keepDeviceSettings: boolean;
};
const operationResult = (r: PoolRemoval, result: GetOperationResponse): PoolRemoval => {
  const target = result.targets.find((t) => t.kind === r.target.kind && t.targetId === r.target.targetId);
  if (!target) throw new Error("Fleet could not load the equipment result.");
  return {
    ...r,
    operationId: result.operationId,
    state: target.state,
    resultCode: target.resultCode,
    keepDeviceSettings: result.kind === "unassign",
  };
};
type Row = { label: string; removal?: PoolRemoval; error?: string; unknown?: boolean };
const isAccepted = (r?: PoolRemoval) => !!r && r.state !== "reviewed";
const canClear = (r?: PoolRemoval) => !!r?.clearSupported && ["verified", "different"].includes(r.verificationState);

const removalProgress = (row: Row, active: boolean, monitoring: boolean, accepted: boolean) => {
  const r = row.removal;
  if (
    r &&
    ["operator_recovery_required", "runtime_pools_remain", "clear_verification_unavailable"].includes(
      r.resultCode ?? "",
    )
  )
    return { kind: "error", message: poolRemovalOutcome(r.state, r.keepDeviceSettings, r.resultCode).message };
  if (r?.state === "completed")
    return {
      kind: "success",
      message: poolRemovalOutcome(r.state, r.keepDeviceSettings).message,
    };
  if (r?.state === "failed" || r?.state === "superseded")
    return {
      kind: "error",
      message: poolRemovalOutcome(r.state, r.keepDeviceSettings).message,
    };
  if (row.unknown || r?.state === "uncertain")
    return {
      kind: "error",
      message: "Fleet has not confirmed removal. Retry this action to check its result.",
    };
  if (row.error) return { kind: "error", message: row.error };
  if (r?.state === "pending" || r?.state === "applying")
    return {
      kind: monitoring ? "waiting" : "idle",
      message: monitoring
        ? "Clearing pools and verifying removal. The Fleet assignment stays until verification completes."
        : "Removal hasn’t been confirmed. Select “Clear pools and remove assignment” to check its result and continue.",
    };
  if (r?.state === "reviewed" && (monitoring || accepted))
    return {
      kind: monitoring ? "waiting" : "idle",
      message: !monitoring
        ? "Not started."
        : active
          ? "Removing pool configuration…"
          : "Waiting for the previous removal to finish.",
    };
};

export const RemoveConfigurationDialog = ({
  assignments,
  assignmentOnly = false,
  password,
  onDismiss,
  onRemoved,
}: {
  assignments: EquipmentAssignment[];
  assignmentOnly?: boolean;
  password: string;
  onDismiss: () => void;
  onRemoved: () => void;
}) => {
  const { client, accountId } = usePoolConfigurationClient();
  const [keepChoice, setKeep] = useState(false);
  const [results, setResults] = useState<Row[]>();
  const [activeIndex, setActiveIndex] = useState<number>();
  const authorizedReviews = useRef(new Set<string>());
  const selection = JSON.stringify(
    assignments
      .filter((a) => a.configurationId)
      .map((a) => ({
        label: a.installationId
          ? `Hashboard ${a.macAddress || a.hashboardId || a.installationId}`
          : a.deviceName || a.deviceIdentifier,
        kind: a.installationId ? "hashboard_installation" : "device",
        targetId: String(a.installationId || a.deviceId),
      })),
  );
  const load = useCallback(
    async (signal: AbortSignal): Promise<Row[]> => {
      const targets = JSON.parse(selection) as { label: string; kind: string; targetId: string }[];
      const rows: Row[] = new Array(targets.length);
      let next = 0;
      await Promise.all(
        Array.from({ length: Math.min(4, targets.length) }, async () => {
          while (next < targets.length && !signal.aborted) {
            const index = next++;
            const target = targets[index];
            try {
              const response = await client.reviewAssignment(
                { accountId, removalTarget: { kind: target.kind, targetId: BigInt(target.targetId) } },
                { signal, timeoutMs: 35_000 },
              );
              if (!response.removal) throw new Error("Fleet could not prepare removal.");
              let removal: PoolRemoval = {
                ...response.removal,
                reviewId: response.reviewId,
                target: { kind: target.kind, targetId: BigInt(target.targetId) },
                state: response.removal.operationId ? "pending" : "reviewed",
              };
              if (removal.operationId) {
                const operation = await client.getOperation(
                  { accountId, operationId: removal.operationId },
                  { signal, timeoutMs: 15_000 },
                );
                removal = operationResult(removal, operation);
              }
              rows[index] = { label: target.label, removal };
            } catch (error) {
              rows[index] = { label: target.label, error: getErrorMessage(error) };
            }
          }
        }),
      );
      return rows;
    },
    [client, accountId, selection],
  );
  const query = useAccountQuery(load);
  const rows = results ?? query.data ?? [];
  const keep =
    rows.find((row) => isAccepted(row.removal))?.removal?.keepDeviceSettings ?? (assignmentOnly || keepChoice);
  const accepted = rows.some((row) => isAccepted(row.removal) || row.unknown);
  const unresolved = rows.some(
    (row) => row.unknown || ["pending", "applying", "uncertain"].includes(row.removal?.state ?? ""),
  );
  const completed = rows.length > 0 && rows.every((row) => row.removal?.state === "completed");
  const valid =
    !query.loading &&
    rows.length > 0 &&
    !completed &&
    !rows.some((row) => row.removal?.resultCode === "operator_recovery_required") &&
    (unresolved ||
      rows.every(
        (row) =>
          !row.error &&
          (row.removal?.state === "completed" ||
            (row.removal?.state === "reviewed" && (keep || canClear(row.removal)))),
      ));
  const submit = async (current: () => boolean) => {
    const reviewIds = rows.flatMap((row) =>
      row.removal?.state === "reviewed" && !row.unknown && !authorizedReviews.current.has(row.removal.reviewId)
        ? [row.removal.reviewId]
        : [],
    );
    if (reviewIds.length) {
      await client.authorizeReviews({ accountId, reviewIds, password }, { timeoutMs: 35_000 });
      reviewIds.forEach((id) => authorizedReviews.current.add(id));
      if (!current()) return;
    }
    const updated = [...rows];
    for (let index = 0; index < rows.length; index++) {
      const row = rows[index];
      const r = row.removal;
      if (!r || r.state === "completed" || r.state === "failed" || r.state === "superseded") continue;
      setActiveIndex(index);
      try {
        let operationId = r.operationId;
        if (!operationId || row.unknown) {
          const response = await client.confirmReview(
            {
              accountId,
              reviewId: r.reviewId,
              keepDeviceSettings: isAccepted(r) ? r.keepDeviceSettings : keep,
              password: "",
            },
            { timeoutMs: 45_000 },
          );
          if (!response.operationId) throw new Error("Fleet could not load the removal result.");
          operationId = response.operationId;
        } else if (r.state === "uncertain" || r.state === "applying") {
          // Recover accepted work by readback; never send a second clear command.
          await client.reviewRetry({ accountId, operationId, targets: [r.target] }, { timeoutMs: 35_000 });
        }
        if (!current()) return;
        updated[index] = {
          label: row.label,
          removal: { ...r, operationId, state: "pending", keepDeviceSettings: keep },
        };
        setResults([...updated]);
        // Closing stops monitoring and later confirmations; accepted work remains
        // owned by the server worker. The same monitor serves assignment progress.
        for await (const operation of pollPoolOperation(client, accountId, operationId, current, 21)) {
          if (!current()) return;
          updated[index] = { label: row.label, removal: operationResult(updated[index].removal!, operation) };
          setResults([...updated]);
          if (updated[index].removal?.resultCode === "operator_recovery_required") break;
        }
        if (updated[index].removal?.state === "completed") {
          refreshPoolAssignments();
          onRemoved();
        }
      } catch (error) {
        const code = ConnectError.from(error).code;
        const unknown =
          row.unknown ||
          [Code.Unknown, Code.Unavailable, Code.DeadlineExceeded, Code.Canceled, Code.Internal, Code.DataLoss].includes(
            code,
          );
        updated[index] = { ...updated[index], error: getErrorMessage(error), unknown };
      }
      if (!current()) return;
      setResults([...updated]);
      // Unresolved work still owns its controller; leave later targets queued.
      if (["pending", "applying", "uncertain"].includes(updated[index].removal?.state ?? "") || updated[index].unknown)
        break;
    }
  };
  return (
    <ActionModal
      title="Remove pool configuration?"
      confirm={keep ? "Remove assignment only" : "Clear pools and remove assignment"}
      allowDismissWhileBusy={accepted}
      confirmVariant="danger"
      cancel={accepted ? "Close" : "Cancel"}
      valid={valid}
      onSubmit={async (current) => {
        setActiveIndex(0);
        try {
          await submit(current);
        } finally {
          if (current()) setActiveIndex(undefined);
        }
      }}
      onDismiss={onDismiss}
    >
      <p>
        {keep
          ? "Remove this equipment’s Fleet assignment. Its current pool settings and mining state will remain unchanged. Future edits to the saved configuration will no longer update this equipment."
          : "Clear all pools currently configured on this equipment and remove its Fleet assignment. The equipment will have no pools configured for mining."}
      </p>
      <p>The saved configuration will remain in your pool library.</p>
      {!assignmentOnly ? (
        <label className="flex items-center gap-2">
          <Checkbox
            checked={keep}
            disabled={accepted}
            onChange={(event) => setKeep(event.target.checked)}
            ariaLabel="Keep the device’s current pool settings"
          />
          Keep the device’s current pool settings
        </label>
      ) : null}
      {query.loading ? (
        <div role="status" className="flex items-center gap-2">
          <ProgressCircular indeterminate size={20} />
          Checking current pool settings…
        </div>
      ) : null}
      {rows.map((row, index) => {
        const r = row.removal;
        const progress = removalProgress(row, activeIndex === index, activeIndex !== undefined, accepted);
        return (
          <div key={index} className="flex flex-col gap-2">
            <p className="font-medium">{row.label}</p>
            {progress ? (
              <div role={progress.kind === "error" ? "alert" : "status"} className="flex items-start gap-2">
                {progress.kind !== "idle" ? (
                  <span aria-hidden="true" className="shrink-0">
                    {progress.kind === "success" ? (
                      <Success className="text-intent-success-fill" />
                    ) : progress.kind === "error" ? (
                      <Alert className="text-intent-critical-fill" />
                    ) : (
                      <ProgressCircular indeterminate size={14} />
                    )}
                  </span>
                ) : null}
                <p>{progress.message}</p>
              </div>
            ) : null}
            {r?.state === "reviewed" && r.verificationState === "different" && !keep ? (
              <p>
                The device’s current pools differ from the Fleet configuration. Clearing pools will also remove those
                device settings. Select “Keep the device’s current pool settings” to preserve them.
              </p>
            ) : null}
            {r?.state === "reviewed" &&
            r.clearSupported &&
            !keep &&
            !["verified", "different"].includes(r.verificationState) ? (
              <p>
                Fleet couldn’t verify the device’s current pool settings. Keep the device’s settings to remove only its
                Fleet assignment, or close and reopen this dialog to check again.
              </p>
            ) : null}
            {r?.state === "reviewed" && !r.clearSupported && !keep ? (
              <p>
                {r.target?.kind === "hashboard_installation"
                  ? "The controller’s current software does not support verified pool clearing for selected hashboards."
                  : "Clearing all pools isn’t supported through Fleet for this equipment or firmware."}{" "}
                Select “Keep the device’s current pool settings” to remove only the Fleet assignment.
              </p>
            ) : null}
          </div>
        );
      })}
    </ActionModal>
  );
};
