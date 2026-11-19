import { useState } from "react";
import ConfigurationEditor from "./ConfigurationEditor";
import { usePoolAssignment } from "./usePoolAssignment";
import type {
  AssignmentPool,
  LocalPoolConfiguration,
  PoolAssignmentPlan,
  PoolAssignmentService,
  PoolAssignmentTarget,
} from "@/protoOS/api/adapters/poolAssignment";
import PoolHashboards from "@/protoOS/features/settings/components/MiningPools/PoolHashboards";
import { Alert } from "@/shared/assets/icons";
import { ActionModal } from "@/shared/components/ActionModal";
import AffectedEquipment from "@/shared/components/AffectedEquipment";
import Button from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import {
  type ConfigurationChange,
  ConfigurationChanges,
} from "@/shared/components/PoolConfigurations/ConfigurationChanges";
import { PoolConfigurationPicker } from "@/shared/components/PoolConfigurations/PoolConfigurationPicker";
import { poolSlotLabel } from "@/shared/components/PoolConfigurations/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const configurationChanges = (original: LocalPoolConfiguration, plan: PoolAssignmentPlan) => {
  const changes: ConfigurationChange[] = [];
  const add = (field: string, before: string, after: string) => {
    if (before !== after) changes.push({ field, before, after });
  };
  add("Name", original.name, plan.name);
  const originalPools = plan.originalPools ?? original.pools;
  for (let i = 0; i < Math.max(originalPools.length, plan.pools.length); i++) {
    const before = originalPools[i];
    const after = plan.pools[i];
    const label = poolSlotLabel(i);
    add(`${label} URL`, before?.url ?? "", after?.url ?? "");
    add(`${label} username`, before?.username ?? "", after?.username ?? "");
    if (before && after && original.isDefault)
      add(`${label} state`, before.enabled ? "Enabled" : "Disabled", after.enabled ? "Enabled" : "Disabled");
    if (after && !after.preservePassword && (after.password || after.id))
      add(`${label} password`, after.id ? "Current setting" : "", after.password ? "New password" : "No password");
  }
  return changes;
};

export default function PoolAssignmentDialog({
  service,
  controllerId,
  target,
  selectedCount = target.modules?.length ?? target.macs.length,
  openStatus = false,
  onDismiss,
  configuration,
  onConfigurationSaved,
  onAssignmentSaved,
}: {
  configuration?: LocalPoolConfiguration | "new";
  onConfigurationSaved?: () => void;
  onAssignmentSaved?: () => void;
  service: PoolAssignmentService;
  controllerId: string;
  target: PoolAssignmentTarget;
  selectedCount?: number;
  openStatus?: boolean;
  onDismiss: () => void;
}) {
  const [creating, setCreating] = useState(Boolean(configuration));
  const [draft, setDraft] = useState<{ name: string; pools: AssignmentPool[] }>();
  const [selectedId, setSelectedId] = useState<string>();
  const original = configuration && configuration !== "new" ? configuration : undefined;
  const state = usePoolAssignment(service, controllerId, target, {
    openStatus,
    onConfigurationSaved: () => {
      setDraft(undefined);
      setCreating(false);
      onConfigurationSaved?.();
    },
    onAssignmentSaved: (status) => {
      pushToast({ message: status.message, status: STATUSES.success });
      onAssignmentSaved?.();
      onDismiss();
    },
  });
  const close = () => {
    if (!state.busy) onDismiss();
  };
  const attempt = state.attempt;
  const reviewingRemaining = state.plan && state.plan.id !== attempt?.id;
  if (attempt && !reviewingRemaining && !(state.busy && attempt.configurationOnly)) {
    const settled = state.status && state.status.state !== "pending";
    const miningCheckRequired = attempt.restoreLegacyMining === true;
    const canReviewRemaining = Boolean(state.status?.remainingMacs?.length && attempt.destinationId);
    let confirm = "Check status";
    let onSubmit: () => void = state.check;
    if (miningCheckRequired) {
      confirm = "Check mining";
      onSubmit = state.checkMiningRecovery;
    } else if (canReviewRemaining) {
      confirm = "Review remaining boards";
      onSubmit = state.reviewRemaining;
    } else if (settled) {
      confirm = "Done";
      onSubmit = () => {
        state.clear();
        if (attempt.configurationOnly) onConfigurationSaved?.();
        else if (state.status?.state === "saved") onAssignmentSaved?.();
        onDismiss();
      };
    }
    return (
      <ActionModal
        title={attempt.configurationOnly ? "Pool configuration status" : "Pool assignment status"}
        confirm={confirm}
        cancel="Close"
        busy={state.busy}
        error={state.error}
        onDismiss={close}
        onSubmit={onSubmit}
      >
        {miningCheckRequired ? (
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title="Container mining was running before this change."
            subtitle="Use the controller's mining controls to start mining when you are ready."
          />
        ) : null}
        <p role="status">{state.busy ? attempt.phase : (state.status?.message ?? "Checking assignment status…")}</p>
        {state.status && !attempt.configurationOnly ? (
          <p>
            Confirmed assignments: {state.status.confirmed} of {state.status.total}
          </p>
        ) : null}
        {state.status?.blockers.map((blocker, index) => (
          <Callout
            key={index}
            intent={intents.warning}
            prefixIcon={<Alert />}
            title={blocker.mac ? `${blocker.mac}: ${blocker.reason}` : blocker.reason}
          />
        ))}
        {state.status && ["partial", "unknown", "failed"].includes(state.status.state) ? (
          <p>Completed changes are kept. Check status before reviewing a new assignment for any remaining boards.</p>
        ) : null}
        {miningCheckRequired ? (
          <Button
            variant="secondary"
            text="Keep stopped"
            disabled={state.busy}
            onClick={() => {
              state.clear();
              if (attempt.configurationOnly) onConfigurationSaved?.();
              close();
            }}
          />
        ) : null}
        {settled && !miningCheckRequired ? (
          <Button variant="secondary" text="Check status" onClick={() => void state.check()} />
        ) : null}
        {canReviewRemaining && !miningCheckRequired ? (
          <Button
            variant="secondary"
            text="Done"
            onClick={() => {
              state.clear();
              close();
            }}
          />
        ) : null}
        {!attempt.configurationOnly ? (
          <PoolHashboards
            boards={attempt.macs.map((mac) => ({ mac }))}
            title="Selected hashboards"
            countLabel="Selected hashboards"
          />
        ) : null}
      </ActionModal>
    );
  }
  if (state.plan) {
    const plan = state.plan;
    const hashboardCount = plan.target.macs.length;
    const hashboardLabel = hashboardCount === 1 ? "hashboard" : "hashboards";
    const editing = plan.editingRevision && hashboardCount ? original : undefined;
    const title = editing
      ? "Review pool configuration changes"
      : plan.configurationOnly
        ? "Enable local pool configurations?"
        : `Assign pool configuration to ${hashboardCount} ${hashboardLabel}?`;
    const description = editing
      ? `Saving this change will update ${hashboardCount} ${hashboardLabel}. Pool changes may briefly interrupt mining.`
      : plan.configurationOnly
        ? `Save ${plan.name}? Hashboard assignments stay unchanged.`
        : `Assign ${plan.name}? Pool changes may briefly interrupt mining.`;
    return (
      <ActionModal
        title={title}
        confirm={editing ? "Save and update equipment" : plan.configurationOnly ? "Save configuration" : "Assign"}
        cancel="Cancel"
        presentation="dialog"
        busy={state.busy}
        error={state.error}
        onDismiss={state.back}
        onSubmit={() => void state.confirm()}
      >
        <p>{description}</p>
        {plan.requiresActivation ? (
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title="Container mining must pause to enable pool assignments."
            subtitle={
              plan.mining
                ? "All hashboards stop. Start mining again when you are ready."
                : "All hashboards must be stopped to continue."
            }
          />
        ) : null}
        {editing ? (
          <>
            <AffectedEquipment
              title="Affected hashboards"
              itemName="hashboards"
              items={plan.boards.map((board) => ({
                id: board.mac,
                label: [board.location, board.mac].filter(Boolean).join(" · "),
              }))}
            />
            <ConfigurationChanges changes={configurationChanges(editing, plan)} />
          </>
        ) : plan.configurationOnly ? (
          <div className="flex flex-col gap-3" data-dd-privacy="mask">
            {plan.pools.map((pool, index) => (
              <div key={index} className="break-all">
                <strong>{pool.name}</strong>
                <p>{pool.url}</p>
                <p>{pool.username}</p>
              </div>
            ))}
          </div>
        ) : null}
      </ActionModal>
    );
  }
  if (creating)
    return (
      <ConfigurationEditor
        draft={draft}
        original={original}
        maxPools={state.inspection?.maxPools ?? 3}
        busy={state.busy}
        error={state.error}
        onDismiss={() => (configuration ? close() : setCreating(false))}
        onSubmit={(name, pools) => {
          setDraft({ name, pools });
          void state.create(name, pools, original);
        }}
      />
    );

  const inspection = state.inspection;
  const options = inspection?.savedPools.map(({ id, name, pools }) => ({ configurationId: id, name, pools }));
  const currentSelectionId = selectedId ?? state.createdId ?? inspection?.currentPoolSetId;
  const selected = options?.find((option) => option.configurationId === currentSelectionId);
  const loadingConfigurations = !inspection && !state.error;
  return (
    <ActionModal
      title="Edit pool"
      initialFocus="dialog"
      confirm="Assign"
      valid={Boolean(selected && !state.error)}
      busy={state.busy}
      onDismiss={close}
      onSubmit={() => {
        if (selected) void state.assign(selected.configurationId, selected.name, selectedCount === 1);
      }}
    >
      <PoolConfigurationPicker
        selectionDescription={`${target.macs.length} ${target.macs.length === 1 ? "hashboard" : "hashboards"} selected.`}
        mixed={inspection?.mixedConfigurations}
        onCreate={() => {
          setSelectedId(undefined);
          setCreating(true);
        }}
        createDisabled={!inspection || state.busy}
        loading={loadingConfigurations}
        error={state.error}
        onRetry={() => void state.inspect()}
        configurations={options}
        selected={selected}
        onSelect={(option) => setSelectedId(option.configurationId)}
      />
    </ActionModal>
  );
}
