import { useCallback, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";
import { Code } from "@connectrpc/connect";

import { usePoolConfigurationClient, usePoolConfigurationPermissions } from "./api";
import { ConfigurationReview, type ConfigurationReviewData } from "./ConfigurationReview";
import { PoolSettingsComparison } from "./PoolSettingsComparison";
import { poolRemovalOutcome, poolStatusExplanation, poolUpdateStatus } from "./status";
import { poolTargetKey, useDevicePoolReadings, usesDevicePoolReadback } from "./useDevicePoolReadings";
import {
  type CheckConfigurationResponse,
  type EquipmentAssignment,
  OperationTargetSchema,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { useAccountMutation } from "@/protoFleet/features/hostedAccounts/useAccountMutation";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import { Alert } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

const keyFor = (target: { kind: string; targetId: bigint }) => `${target.kind}:${target.targetId}`;
const allocationAccessMessage =
  "You don’t have access to view this hardware’s pool configuration. Pool configurations are only visible in the account the hardware is allocated to.";
const assignmentTarget = (a: EquipmentAssignment) => ({
  kind: a.installationId ? "hashboard_installation" : "device",
  targetId: a.installationId || a.deviceId,
});

const isNewerThan = (evidence: Timestamp | undefined, checkedAt: Timestamp | undefined) =>
  !!evidence &&
  (!checkedAt ||
    evidence.seconds > checkedAt.seconds ||
    (evidence.seconds === checkedAt.seconds && evidence.nanos > checkedAt.nanos));

export const OperationResults = ({
  operationId,
  assignments = [],
  selectedTargets,
  unavailable = false,
  accessDenied = false,
  loading = false,
  onRefresh,
  onOperationChange,
  onDismiss,
}: {
  operationId?: string;
  assignments?: EquipmentAssignment[];
  selectedTargets?: { kind: string; targetId: bigint }[];
  unavailable?: boolean;
  accessDenied?: boolean;
  loading?: boolean;
  onRefresh?: () => void;
  onOperationChange: (id: string) => void;
  onDismiss: () => void;
}) => {
  const { client, accountId, readOnly } = usePoolConfigurationClient();
  const { change } = usePoolConfigurationPermissions();
  const [replacement, setReplacement] = useState("");
  const ids = [
    ...new Set(
      replacement ? [replacement] : operationId ? [operationId] : assignments.map((a) => a.operationId).filter(Boolean),
    ),
  ]
    .sort()
    .join(",");
  const load = useCallback(
    async (signal: AbortSignal) =>
      Promise.all(
        ids
          .split(",")
          .filter(Boolean)
          .map((id) =>
            client.getOperation({ accountId, operationId: id }, { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS }),
          ),
      ),
    [client, accountId, ids],
  );
  const query = useAccountQuery(load, !!ids && !accessDenied, true, 3000);
  const mutation = useAccountMutation();
  const [review, setReview] = useState<ConfigurationReviewData>();
  const [checks, setChecks] = useState<Record<string, CheckConfigurationResponse>>({});
  const deviceReadings = useDevicePoolReadings(client, accountId, assignments, change && !readOnly && !accessDenied);
  const refresh = () => {
    query.refresh();
    deviceReadings.refresh();
    onRefresh?.();
  };
  const selected = selectedTargets && new Set(selectedTargets.filter((t) => t.targetId > 0n).map(keyFor));
  const explicitOperation = !!(replacement || operationId);
  const currentAssignments = new Map(assignments.map((a) => [keyFor(assignmentTarget(a)), a]));
  // Board rows can refer to a whole-container operation. Match both the
  // controller and current operation so unrelated or replaced work stays hidden.
  const projectedAssignment = (target: { kind: string; targetId: bigint }, id: string) =>
    target.kind === "device"
      ? assignments.find(
          (a) =>
            a.installationId &&
            a.deviceId === target.targetId &&
            a.operationId === id &&
            (!selected || selected.has(keyFor(assignmentTarget(a)))),
        )
      : undefined;
  const rows = (query.data ?? []).flatMap((operation) =>
    operation.targets
      .filter((target) => {
        const key = keyFor(target);
        return (
          ((!selected || selected.has(key)) &&
            (explicitOperation || currentAssignments.get(key)?.operationId === operation.operationId)) ||
          !!projectedAssignment(target, operation.operationId)
        );
      })
      .map((target) => ({
        target,
        operationKind: operation.kind,
        configurationId: operation.configurationId,
        name: operation.configurationName,
        operationId: operation.operationId,
        createdAt: operation.createdAt,
      })),
  );
  for (const a of assignments.filter((a) => !explicitOperation && !a.operationId)) {
    rows.push({
      target: create(OperationTargetSchema, {
        ...assignmentTarget(a),
        state: a.state,
        resultCode: a.resultCode,
        deviceIdentifier: a.deviceIdentifier,
        label: a.deviceName,
        macAddress: a.macAddress,
        updatedAt: a.updatedAt,
      }),
      operationKind: "apply",
      configurationId: a.configurationId,
      name: a.name,
      operationId: "",
      createdAt: undefined,
    });
  }
  if (accessDenied)
    return (
      <Modal
        open
        title="Pool configuration"
        dialogLabel="Pool configuration"
        manageFocus
        initialFocus="dialog"
        onDismiss={mutation.busy ? undefined : onDismiss}
      >
        <p>{allocationAccessMessage}</p>
      </Modal>
    );
  if (review)
    return (
      <ConfigurationReview
        review={review}
        onDismiss={() => setReview(undefined)}
        onApplied={(_, id) => {
          setReview(undefined);
          setChecks({});
          setReplacement(id);
          onOperationChange(id);
          onRefresh?.();
        }}
      />
    );
  return (
    <Modal
      open
      title="Pool configuration"
      dialogLabel="Pool configuration"
      manageFocus
      initialFocus="dialog"
      onDismiss={mutation.busy ? undefined : onDismiss}
    >
      <div className="flex flex-col gap-4">
        {query.loading || loading ? <ProgressCircular indeterminate /> : null}
        {query.error || unavailable ? (
          <div role="alert">
            <p>
              {query.errorCode === Code.PermissionDenied
                ? "You don’t have permission to view these results. You need access to the pool configuration and all equipment included in this update. Ask your administrator for access."
                : "Fleet could not load the update status. The update may still be running."}
            </p>
            <Button variant="secondary" text="Refresh status" onClick={refresh} />
          </div>
        ) : null}
        {mutation.error ? <p role="alert">{mutation.error}</p> : null}
        {rows.map((row) => {
          const removal = row.operationKind === "clear" || row.operationKind === "unassign";
          const key = keyFor(row.target);
          const currentAssignment = currentAssignments.get(key) ?? projectedAssignment(row.target, row.operationId);
          const assignmentChanged =
            !!currentAssignment &&
            (currentAssignment.operationId !== row.operationId ||
              currentAssignment.configurationId !== row.configurationId);
          const assignment = assignmentChanged ? undefined : currentAssignment;
          const checkKey = `${accountId}:${row.configurationId}:${row.operationId}:${key}`;
          // Historical operation results remain historical. A cached read must
          // never override replacement or newer persisted observation evidence.
          const cached = checks[checkKey];
          const serverResult = removal || row.target.state === "superseded" ? row.target : (assignment ?? row.target);
          const check =
            !removal && !assignmentChanged && serverResult.state !== "superseded"
              ? ((cached &&
                !isNewerThan(row.target.updatedAt, cached.checkedAt) &&
                !isNewerThan(assignment?.updatedAt, cached.checkedAt)
                  ? cached
                  : undefined) ??
                (assignment && usesDevicePoolReadback(assignment)
                  ? deviceReadings.readings[poolTargetKey(assignment)]
                  : undefined))
              : undefined;
          const result = check ?? serverResult;
          const outcome = removal
            ? poolRemovalOutcome(result.state, row.operationKind === "unassign", result.resultCode)
            : undefined;
          const status = outcome?.status ?? poolUpdateStatus(result);
          const canCheck =
            !removal &&
            change &&
            !readOnly &&
            !assignmentChanged &&
            !["pending", "applying", "superseded"].includes(result.state);
          const canRetry =
            !removal &&
            change &&
            !readOnly &&
            !assignmentChanged &&
            row.operationId &&
            ["failed", "different", "applied_unverified"].includes(result.state) &&
            result.resultCode !== "write_uncertain";
          const checkConfiguration = () =>
            void mutation.run(async () => {
              const checked = await client.checkConfiguration(
                {
                  accountId,
                  configurationId: row.configurationId,
                  target: { kind: row.target.kind, targetId: row.target.targetId },
                },
                { timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
              );
              setChecks((values) => ({ ...values, [checkKey]: checked }));
              onRefresh?.();
            });
          const lastVerified = [
            poolUpdateStatus(row.target) === "Verified" ? row.target.updatedAt : undefined,
            assignment && poolUpdateStatus(assignment) === "Verified" ? assignment.updatedAt : undefined,
            check && poolUpdateStatus(check) === "Verified" ? check.checkedAt : undefined,
          ].reduce<Timestamp | undefined>((latest, time) => (isNewerThan(time, latest) ? time : latest), undefined);
          const blocked =
            !removal && !assignmentChanged && result.state !== "superseded" && result.resultCode === "write_uncertain";
          const readable = check?.readbackAvailable || (check && check.reported.length > 0);
          const name =
            assignment?.deviceName ||
            row.target.label ||
            `${row.target.kind === "device" ? "Miner" : "Hashboard"} ${row.target.targetId}`;
          const explanation =
            outcome?.message ??
            (blocked && result.state !== "different" && readable
              ? "The reported settings match, but Fleet couldn’t confirm that an earlier update finished."
              : poolStatusExplanation(result));
          return (
            <section
              key={`${row.operationId}:${key}`}
              aria-label={name}
              className="flex flex-col gap-6 border-b border-border-10 pb-6 last:border-b-0 last:pb-0"
            >
              <div>
                <h2 className="text-heading-200 [overflow-wrap:anywhere]">{name}</h2>
                {row.configurationId ? (
                  <p className="mt-1 text-300 text-text-primary-50">
                    Configuration: <span className="text-text-primary">{assignment?.name || row.name}</span>
                  </p>
                ) : null}
              </div>
              <div className="flex flex-col gap-1.5">
                <h3 className="text-heading-200">{status}</h3>
                <p className="text-300 text-text-primary-70">{explanation}</p>
              </div>
              {readable && check ? (
                <div className="flex flex-col gap-2">
                  <PoolSettingsComparison
                    expected={check.expected}
                    reported={check.reported}
                    assigned={!!row.configurationId}
                  />
                  {check.checkedAt ? (
                    <p className="text-200 text-text-primary-50">
                      Last checked: {timestampDate(check.checkedAt).toLocaleString()}
                    </p>
                  ) : null}
                </div>
              ) : null}
              {row.createdAt || lastVerified ? (
                <div className="text-200">
                  <h3 className="text-emphasis-200 text-text-primary-70">Details</h3>
                  <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-text-primary-50">
                    {row.createdAt ? (
                      <>
                        <dt>Update requested</dt>
                        <dd>{timestampDate(row.createdAt).toLocaleString()}</dd>
                      </>
                    ) : null}
                    {lastVerified ? (
                      <>
                        <dt>Last verified</dt>
                        <dd>{timestampDate(lastVerified).toLocaleString()}</dd>
                      </>
                    ) : null}
                  </dl>
                </div>
              ) : null}
              {blocked || canCheck || canRetry ? (
                <div className="flex flex-col gap-4">
                  {blocked ? (
                    <Callout
                      intent="warning"
                      prefixIcon={<Alert />}
                      title="Reapply unavailable"
                      subtitle="Fleet couldn’t confirm that an earlier device update finished. Another pool update is blocked until that is resolved."
                    />
                  ) : null}
                  <div className="flex flex-wrap items-center justify-end gap-3">
                    {canCheck ? (
                      <Button
                        variant="secondary"
                        text={check ? "Check again" : "Check configuration"}
                        loading={mutation.busy}
                        onClick={checkConfiguration}
                      />
                    ) : null}
                    {canRetry || (blocked && change && !readOnly) ? (
                      <Button
                        variant="primary"
                        text={blocked || result.state === "different" ? "Reapply" : "Retry"}
                        disabled={mutation.busy || blocked}
                        onClick={() =>
                          void mutation.run(async () =>
                            setReview(
                              await client.reviewRetry({
                                accountId,
                                operationId: row.operationId,
                                targets: [{ kind: row.target.kind, targetId: row.target.targetId }],
                              }),
                            ),
                          )
                        }
                      />
                    ) : null}
                  </div>
                </div>
              ) : null}
            </section>
          );
        })}
        {!loading && !ids && !rows.length && !unavailable ? <p>{allocationAccessMessage}</p> : null}
      </div>
    </Modal>
  );
};
