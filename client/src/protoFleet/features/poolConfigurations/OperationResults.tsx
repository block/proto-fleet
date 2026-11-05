import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";
import { Code } from "@connectrpc/connect";

import { usePoolConfigurationClient, usePoolConfigurationPermissions } from "./api";
import { ConfigurationReview, type ConfigurationReviewData } from "./ConfigurationReview";
import { PoolSettingsComparison } from "./PoolSettingsComparison";
import { poolStatusExplanation, poolUpdateStatus } from "./status";
import { poolTargetKey, useDevicePoolReadings } from "./useDevicePoolReadings";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import { MinerIdentifierType } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  type CheckConfigurationResponse,
  type EquipmentAssignment,
  OperationTargetSchema,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { lookupMinerByIdentifier, type LookupMinerResult } from "@/protoFleet/api/lookupMinerByIdentifier";
import { useOpenMinerView } from "@/protoFleet/components/SingleMinerWrapper/useOpenMinerView";
import { containerDetailPath } from "@/protoFleet/features/containers/useOpenContainerView";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { useAccountMutation } from "@/protoFleet/features/hostedAccounts/useAccountMutation";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import { useFleetStore } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

const keyFor = (target: { kind: string; targetId: bigint }) => `${target.kind}:${target.targetId}`;
const assignmentTarget = (a: EquipmentAssignment) => ({
  kind: a.installationId ? "hashboard_installation" : "device",
  targetId: a.installationId || a.deviceId,
});

const isNewerThan = (evidence: Timestamp | undefined, checkedAt: Timestamp | undefined) =>
  !!evidence &&
  (!checkedAt ||
    evidence.seconds > checkedAt.seconds ||
    (evidence.seconds === checkedAt.seconds && evidence.nanos > checkedAt.nanos));

// Scope shared lookups to this results view and session. Each controller resolves
// independently so a slow controller does not block navigation to the others.
const useEquipmentLookups = (identifiers: string[]) => {
  const selection = JSON.stringify([...new Set(identifiers)].sort());
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const [result, setResult] = useState<{
    selection: string;
    generation: number;
    lookups: Map<string, LookupMinerResult>;
  }>();
  useEffect(() => {
    const controller = new AbortController();
    for (const identifier of JSON.parse(selection) as string[]) {
      void lookupMinerByIdentifier(identifier, MinerIdentifierType.DEVICE_IDENTIFIER, controller.signal).then(
        (lookup) => {
          if (controller.signal.aborted) return;
          setResult((previous) => ({
            selection,
            generation,
            lookups: new Map(
              previous?.selection === selection && previous.generation === generation ? previous.lookups : [],
            ).set(identifier, lookup),
          }));
        },
      );
    }
    return () => controller.abort();
  }, [selection, generation]);
  return result?.selection === selection && result.generation === generation
    ? result.lookups
    : new Map<string, LookupMinerResult>();
};

const EquipmentNavigation = ({ lookup }: { lookup?: LookupMinerResult }) => {
  const openMinerView = useOpenMinerView();
  const miner = lookup?.status === "found" ? lookup.snapshot : undefined;
  if (miner?.deviceClass === DeviceClass.CONTAINER)
    return <Button variant="textOnly" text="View container" className="mr-auto" to={containerDetailPath(miner)} />;
  if (lookup && (!miner || (!miner.embeddedWebViewAvailable && !miner.url)))
    return <span className="mr-auto text-200 text-text-primary-50">Equipment view unavailable</span>;
  return (
    <Button
      variant="textOnly"
      text="View miner"
      className="mr-auto"
      loading={!lookup}
      onClick={() => miner && openMinerView(miner)}
    />
  );
};

export const OperationResults = ({
  operationId,
  assignments = [],
  selectedTargets,
  unavailable = false,
  loading = false,
  onRefresh,
  onOperationChange,
  onDismiss,
}: {
  operationId?: string;
  assignments?: EquipmentAssignment[];
  selectedTargets?: { kind: string; targetId: bigint }[];
  unavailable?: boolean;
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
  const query = useAccountQuery(load, !!ids, true, 3000);
  const mutation = useAccountMutation();
  const [review, setReview] = useState<ConfigurationReviewData>();
  const [checks, setChecks] = useState<Record<string, CheckConfigurationResponse>>({});
  const deviceReadings = useDevicePoolReadings(client, accountId, assignments, change && !readOnly);
  const refresh = () => {
    query.refresh();
    deviceReadings.refresh();
    onRefresh?.();
  };
  const selected = selectedTargets && new Set(selectedTargets.filter((t) => t.targetId > 0n).map(keyFor));
  const explicitOperation = !!(replacement || operationId);
  const currentAssignments = new Map(assignments.map((a) => [keyFor(assignmentTarget(a)), a]));
  const rows = (query.data ?? []).flatMap((operation) =>
    operation.targets
      .filter((target) => {
        const key = keyFor(target);
        return (
          (!selected || selected.has(key)) &&
          (explicitOperation || currentAssignments.get(key)?.operationId === operation.operationId)
        );
      })
      .map((target) => ({
        target,
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
      configurationId: a.configurationId,
      name: a.name,
      operationId: "",
      createdAt: undefined,
    });
  }
  const equipmentLookups = useEquipmentLookups(
    accountId || review ? [] : rows.map((row) => row.target.deviceIdentifier).filter(Boolean),
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
          const key = keyFor(row.target);
          const currentAssignment = currentAssignments.get(key);
          const assignmentChanged =
            !!currentAssignment &&
            (currentAssignment.operationId !== row.operationId ||
              currentAssignment.configurationId !== row.configurationId);
          const assignment = assignmentChanged ? undefined : currentAssignment;
          const checkKey = `${accountId}:${row.configurationId}:${row.operationId}:${key}`;
          // Historical operation results remain historical. A cached read must
          // never override replacement or newer persisted observation evidence.
          const cached = checks[checkKey];
          const serverResult = row.target.state === "superseded" ? row.target : (assignment ?? row.target);
          const check =
            !assignmentChanged && serverResult.state !== "superseded"
              ? ((cached &&
                !isNewerThan(row.target.updatedAt, cached.checkedAt) &&
                !isNewerThan(assignment?.updatedAt, cached.checkedAt)
                  ? cached
                  : undefined) ??
                (assignment && !assignment.configurationId
                  ? deviceReadings.readings[poolTargetKey(assignment)]
                  : undefined))
              : undefined;
          const result = check ?? serverResult;
          const status = poolUpdateStatus(result);
          const canCheck =
            change && !readOnly && !assignmentChanged && !["pending", "applying", "superseded"].includes(result.state);
          const canRetry =
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
          const lastVerified = row.target.state === "verified" ? row.target.updatedAt : undefined;
          const blocked =
            !assignmentChanged && result.state !== "superseded" && result.resultCode === "write_uncertain";
          const readable = check?.readbackAvailable || (check && check.reported.length > 0);
          const name =
            assignment?.deviceName ||
            row.target.label ||
            `${row.target.kind === "device" ? "Miner" : "Hashboard"} ${row.target.targetId}`;
          const explanation =
            blocked && result.state !== "different" && readable
              ? "The reported settings match, but Fleet couldn’t confirm that an earlier update finished."
              : poolStatusExplanation(result);
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
                    Configuration: <span className="text-text-primary">{row.name || assignment?.name}</span>
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
              <div className="flex flex-col gap-4 border-t border-border-10 pt-4">
                {blocked ? (
                  <Callout
                    intent="warning"
                    prefixIcon={<Alert />}
                    title="Reapply unavailable"
                    subtitle="Fleet couldn’t confirm that an earlier device update finished. Another pool update is blocked until that is resolved."
                  />
                ) : null}
                <div className="flex flex-wrap items-center justify-end gap-3">
                  {row.target.deviceIdentifier && !accountId ? (
                    <EquipmentNavigation lookup={equipmentLookups.get(row.target.deviceIdentifier)} />
                  ) : null}
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
            </section>
          );
        })}
        {!loading && !ids && !rows.length && !unavailable ? (
          <p>No saved pool configuration is assigned. The equipment may still have pools configured.</p>
        ) : null}
      </div>
    </Modal>
  );
};
