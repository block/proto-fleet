import { useCallback, useRef, useState } from "react";

import { usePoolConfigurationClient, usePoolConfigurationPermissions } from "./api";
import { trackPoolOperation } from "./assignmentProgress";
import { ConfigurationEditor } from "./ConfigurationEditor";
import { ConfigurationOptions } from "./ConfigurationOptions";
import type { ConfigurationReviewData } from "./ConfigurationReview";
import { ConfirmationError, useReviewedConfirmation } from "./useReviewedConfirmation";
import type { MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { Configuration } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import { useFleetStore } from "@/protoFleet/store";
import { Plus } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";

export type BoardSelectionInput = { moduleIds?: bigint[]; hashboardIds?: bigint[] };

type ConfigurationSelectorProps = {
  boards?: BoardSelectionInput;
  deviceIdentifiers?: string[];
  deviceIds?: bigint[];
  filter?: MinerListFilter;
  selectedCount?: number;
  selectionDescription?: string;
  onDismiss: () => void;
  onApplied?: (operationId: string) => void;
};

export const ConfigurationSelector = (props: ConfigurationSelectorProps) => {
  const { accountId, readOnly } = usePoolConfigurationClient();
  const { change } = usePoolConfigurationPermissions();
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  if (!change || readOnly) return null;
  return <AuthenticatedConfigurationSelector key={`${accountId}:${generation}`} {...props} />;
};

const AuthenticatedConfigurationSelector = (props: ConfigurationSelectorProps) => {
  const [password, setPassword] = useState("");
  const dismiss = () => {
    setPassword("");
    props.onDismiss();
  };
  if (!password)
    return (
      <AuthenticateFleetModal
        open
        purpose="pool"
        onAuthenticated={(_username, verifiedPassword) => setPassword(verifiedPassword)}
        onDismiss={dismiss}
      />
    );
  return <ConfigurationPicker {...props} password={password} onDismiss={dismiss} />;
};

const ConfigurationPicker = ({
  deviceIdentifiers = [],
  deviceIds = [],
  boards,
  filter,
  selectedCount,
  selectionDescription,
  onDismiss,
  onApplied,
  password,
}: ConfigurationSelectorProps & { password: string }) => {
  const { client, accountId, readOnly } = usePoolConfigurationClient();
  const scope = useAccountScope();
  const { manage, change } = usePoolConfigurationPermissions();
  const [choice, setSelected] = useState<Configuration>();
  const [creating, setCreating] = useState(false);
  const [review, setReview] = useState<ConfigurationReviewData>();
  const confirmation = useReviewedConfirmation();
  const [uncertain, setUncertain] = useState(false);
  // Retain the review after a lost confirmation response so retry is idempotent.
  const pendingReview = useRef<
    | {
        key: string;
        filter?: MinerListFilter;
        review: ConfigurationReviewData;
      }
    | undefined
  >(undefined);
  const singleSelection =
    !filter &&
    deviceIdentifiers.length +
      deviceIds.length +
      (boards?.moduleIds?.length ?? 0) +
      (boards?.hashboardIds?.length ?? 0) ===
      1;
  const selectionKey = JSON.stringify({
    deviceIdentifiers,
    deviceIds: deviceIds.map(String),
    moduleIds: boards?.moduleIds?.map(String),
    hashboardIds: boards?.hashboardIds?.map(String),
  });
  const load = useCallback(
    async (signal: AbortSignal) => {
      const configurations: Configuration[] = [];
      let pageToken = "";
      do {
        const page = await client.listConfigurations(
          { accountId, pageToken, pageSize: 100 },
          { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
        );
        configurations.push(...page.configurations);
        pageToken = page.nextPageToken;
      } while (pageToken);
      const selection = JSON.parse(selectionKey) as {
        deviceIdentifiers: string[];
        deviceIds: string[];
        moduleIds?: string[];
        hashboardIds?: string[];
      };
      const current = await client.getAssignments(
        {
          accountId,
          deviceIdentifiers: filter ? [] : selection.deviceIdentifiers,
          deviceIds: filter ? [] : selection.deviceIds.map(BigInt),
          filter,
          boards:
            selection.moduleIds || selection.hashboardIds
              ? { moduleIds: selection.moduleIds?.map(BigInt), hashboardIds: selection.hashboardIds?.map(BigInt) }
              : undefined,
        },
        { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
      );
      return { configurations, current };
    },
    [client, accountId, selectionKey, filter],
  );
  const query = useAccountQuery(load);
  const current = query.data?.current;
  const commonId =
    current &&
    current.selectedCount > 0n &&
    BigInt(current.assignments.length) === current.selectedCount &&
    current.assignments.every((item) => item.configurationId === current.assignments[0].configurationId)
      ? current.assignments[0].configurationId
      : undefined;
  const selected = choice ?? query.data?.configurations.find((item) => item.configurationId === commonId);
  const mixed = current && current.assignments.some((item) => item.configurationId) && !commonId;
  const isCurrent = confirmation.current;
  const dismiss = () => {
    confirmation.close();
    onDismiss();
  };
  const confirmAssignment = async (reviewed: ConfigurationReviewData) => {
    if (!isCurrent()) return;
    let saved;
    try {
      saved = await confirmation.confirm(
        {
          reviewId: reviewed.reviewId,
          configurationId: reviewed.configuration?.configurationId ?? "",
          name: reviewed.configuration?.name ?? "Pool configuration",
          kind: "pool_configuration",
          deviceIdentifiers: (reviewed.equipment ?? [])
            .filter((item) => item.kind === "device")
            .map((item) => item.deviceIdentifier),
        },
        password,
      );
    } catch (error) {
      if (!isCurrent()) return;
      const unknown = error instanceof ConfirmationError && error.uncertain;
      setUncertain(unknown);
      if (unknown) setReview(reviewed);
      else {
        pendingReview.current = undefined;
        setReview(undefined);
      }
      throw error;
    }
    if (!saved || !isCurrent()) return;
    if (saved.operationId)
      void trackPoolOperation(
        client,
        accountId,
        saved.configuration,
        saved.operationId,
        reviewed.equipment ?? [],
        scope?.signal,
      );
    onApplied?.(saved.operationId);
    dismiss();
  };
  if (!change || readOnly) return null;
  if (review) {
    const counts = [
      review.affectedMiners > 0n ? `${review.affectedMiners} miner${review.affectedMiners === 1n ? "" : "s"}` : "",
      review.affectedHashboards > 0n
        ? `${review.affectedHashboards} hashboard${review.affectedHashboards === 1n ? "" : "s"}`
        : "",
    ]
      .filter(Boolean)
      .join(" and ");
    return (
      <ActionModal
        title={`Assign pool configuration to ${counts || "selected equipment"}?`}
        confirm={uncertain ? "Retry confirmation" : "Assign"}
        cancel={uncertain ? "Close" : "Cancel"}
        presentation="dialog"
        onDismiss={() => {
          if (uncertain) return dismiss();
          setReview(undefined);
          pendingReview.current = undefined;
        }}
        onSubmit={() => confirmAssignment(review)}
      >
        {selectionDescription ? <p>{selectionDescription}</p> : null}
        <p>Assign {review.configuration?.name ?? "this configuration"}? Pool changes may briefly interrupt mining.</p>
      </ActionModal>
    );
  }
  if (creating)
    return (
      <ConfigurationEditor
        onDismiss={() => setCreating(false)}
        onSaved={(configuration) => {
          setSelected(configuration);
          query.refresh();
        }}
      />
    );
  return (
    <ActionModal
      title="Edit pool"
      initialFocus="dialog"
      confirm="Assign"
      valid={Boolean(
        selected &&
        password &&
        !query.loading &&
        !query.error &&
        query.data?.configurations.some(
          (item) => item.configurationId === selected.configurationId && item.version === selected.version,
        ) &&
        (filter || boards || deviceIdentifiers.length + deviceIds.length > 0),
      )}
      onDismiss={dismiss}
      onSubmit={async () => {
        if (!selected) return;
        const key = `${selectionKey}:${selected.configurationId}:${selected.version}`;
        const pending = pendingReview.current;
        const reviewed =
          pending?.key === key && pending.filter === filter
            ? pending.review
            : await client.reviewAssignment(
                {
                  accountId,
                  configurationId: selected.configurationId,
                  expectedVersion: selected.version,
                  deviceIdentifiers: filter ? [] : deviceIdentifiers,
                  deviceIds: filter ? [] : deviceIds,
                  filter,
                  boards,
                },
                { signal: scope?.signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
              );
        if (!isCurrent()) return;
        pendingReview.current = { key, filter, review: reviewed };
        if (singleSelection) await confirmAssignment(reviewed);
        else setReview(reviewed);
      }}
    >
      <p>
        {selectionDescription ??
          (boards
            ? `${deviceIds.length ? `${deviceIds.length} miners and ` : ""}${(current?.selectedCount ?? BigInt(deviceIds.length)) - BigInt(deviceIds.length)} hashboards selected. Selected boards will start mining with this configuration.`
            : filter && selectedCount === undefined
              ? "Existing paired miners"
              : `${selectedCount ?? deviceIdentifiers.length + deviceIds.length} miners selected`)}
      </p>
      {mixed ? <p>Mixed configurations</p> : null}
      {manage ? (
        <div>
          <Button
            variant="secondary"
            prefixIcon={<Plus />}
            text="New pool configuration"
            onClick={() => {
              setCreating(true);
            }}
          />
        </div>
      ) : null}
      {query.loading ? (
        <div className="flex justify-center py-10">
          <ProgressCircular indeterminate />
        </div>
      ) : null}
      {query.error ? (
        <div role="alert">
          <p>{query.error}</p>
          <Button variant="secondary" text="Retry" onClick={query.refresh} />
        </div>
      ) : null}
      {query.data ? (
        <ConfigurationOptions configurations={query.data.configurations} selected={selected} onSelect={setSelected} />
      ) : null}
    </ActionModal>
  );
};
