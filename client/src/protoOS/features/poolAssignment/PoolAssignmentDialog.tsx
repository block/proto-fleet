import { useState } from "react";
import ConfigurationEditor from "./ConfigurationEditor";
import { usePoolAssignment } from "./usePoolAssignment";
import type {
  AssignmentPool,
  PoolAssignmentService,
  PoolAssignmentTarget,
  SavedPoolSet,
} from "@/protoOS/api/adapters/poolAssignment";
import PoolHashboards from "@/protoOS/features/settings/components/MiningPools/PoolHashboards";
import { Alert, Plus } from "@/shared/assets/icons";
import { ActionModal } from "@/shared/components/ActionModal";
import AffectedEquipment from "@/shared/components/AffectedEquipment";
import Button from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import { ConfigurationOptions } from "@/shared/components/PoolConfigurations/ConfigurationOptions";
import ProgressCircular from "@/shared/components/ProgressCircular";

export default function PoolAssignmentDialog({
  service,
  controllerId,
  target,
  onDismiss,
  configuration,
  onConfigurationSaved,
}: {
  configuration?: SavedPoolSet | "new";
  onConfigurationSaved?: () => void;
  service: PoolAssignmentService;
  controllerId: string;
  target: PoolAssignmentTarget;
  onDismiss: () => void;
}) {
  const [creating, setCreating] = useState(Boolean(configuration));
  const [draft, setDraft] = useState<{ name: string; pools: AssignmentPool[] }>();
  const [selectedId, setSelectedId] = useState<string>();
  const original = configuration && configuration !== "new" ? configuration : undefined;
  const state = usePoolAssignment(service, controllerId, target, () => {
    setDraft(undefined);
    onConfigurationSaved?.();
  });
  const options = (state.inspection?.savedPools ?? []).map((set) => ({
    ...set,
    configurationId: set.id,
  }));
  const selected = options.find(
    (option) => option.configurationId === (selectedId ?? state.createdId ?? state.inspection?.currentPoolSetId),
  );
  const close = () => {
    if (!state.busy) onDismiss();
  };
  const reviewingRemaining = state.plan && state.plan.id !== state.attempt?.id;
  if (state.attempt && !reviewingRemaining && !(state.busy && state.attempt.configurationOnly)) {
    const settled = state.status && state.status.state !== "pending";
    const restoreRequired = state.attempt.restoreLegacyMining === true;
    let confirm = "Check status";
    if (restoreRequired) confirm = "Restore mining";
    else if (state.status?.remainingMacs?.length && state.attempt.destinationId) confirm = "Review remaining boards";
    else if (settled) confirm = "Done";
    return (
      <ActionModal
        title={state.attempt.configurationOnly ? "Pool configuration status" : "Pool assignment status"}
        confirm={confirm}
        cancel="Close"
        busy={state.busy}
        error={state.error}
        onDismiss={close}
        onSubmit={() => {
          if (restoreRequired) void state.restoreMining();
          else if (state.status?.remainingMacs?.length && state.attempt.destinationId) void state.reviewRemaining();
          else if (settled) {
            state.clear();
            if (state.attempt?.configurationOnly) onConfigurationSaved?.();
            onDismiss();
          } else void state.check();
        }}
      >
        {restoreRequired ? (
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title="Container mining was running before this change."
            subtitle="Restore mining before you clear this recovery record."
          />
        ) : null}
        <p role="status">
          {state.busy ? state.attempt.phase : (state.status?.message ?? "Checking assignment status…")}
        </p>
        {state.status && !state.attempt.configurationOnly ? (
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
        {settled ? <Button variant="secondary" text="Check status" onClick={() => void state.check()} /> : null}
        {state.status?.remainingMacs?.length && state.attempt.destinationId ? (
          <Button
            variant="secondary"
            text="Done"
            onClick={() => {
              state.clear();
              close();
            }}
          />
        ) : null}
        {!state.attempt.configurationOnly ? (
          <PoolHashboards
            boards={state.attempt.macs.map((mac) => ({ mac }))}
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
    const title = plan.configurationOnly
      ? "Enable local pool configurations?"
      : `Assign pool configuration to ${hashboardCount} ${hashboardLabel}?`;
    const description = plan.configurationOnly
      ? `Save ${plan.name}? Hashboard assignments stay unchanged.`
      : `Assign ${plan.name}? Pool changes may interrupt mining.`;
    return (
      <ActionModal
        title={title}
        confirm={plan.configurationOnly ? "Save configuration" : "Assign"}
        cancel="Cancel"
        presentation="dialog"
        busy={state.busy}
        error={state.error}
        onDismiss={() => {
          state.back();
          if (plan.configurationOnly && draft) setCreating(true);
        }}
        onSubmit={() => void state.confirm()}
      >
        <p>{description}</p>
        {plan.requiresActivation ? (
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title="Container mining must pause to enable pool assignments."
            subtitle="All hashboards stop. The previous default mining intent is restored before selected boards move."
          />
        ) : null}
        <div className="flex flex-col gap-3" data-dd-privacy="mask">
          {(plan.savedPoolSet?.pools ?? plan.pools).map((pool, index) => (
            <div key={index} className="break-all">
              <strong>{pool.name}</strong>
              <p>{pool.url}</p>
              <p>{pool.username}</p>
            </div>
          ))}
        </div>
        {!plan.configurationOnly ? (
          <AffectedEquipment
            title="Affected hashboards"
            itemName="hashboards"
            items={plan.boards.map((board) => ({
              id: board.mac,
              label: [board.location, board.mac].filter(Boolean).join(" · "),
            }))}
          />
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
        onSaved={async (name, pools) => {
          setDraft({ name, pools });
          const saved = await state.create(name, pools, original);
          if (saved) setCreating(false);
          return Boolean(saved);
        }}
      />
    );

  return (
    <ActionModal
      title="Edit pool"
      initialFocus="dialog"
      confirm="Assign"
      valid={Boolean(selected && state.inspection && !state.error)}
      busy={state.busy}
      error={state.error}
      onDismiss={close}
      onSubmit={() => {
        if (selected) void state.review([], selected.configurationId, target.macs, selected.name);
      }}
    >
      <p>
        {target.macs.length} {target.macs.length === 1 ? "hashboard" : "hashboards"} selected.
      </p>
      <div className="flex flex-wrap gap-3">
        <Button
          variant="secondary"
          prefixIcon={<Plus />}
          text="New pool configuration"
          disabled={!state.inspection || state.busy}
          onClick={() => {
            setSelectedId(undefined);
            setCreating(true);
          }}
        />
      </div>
      {!state.inspection && !state.error ? (
        <div className="flex justify-center py-10">
          <ProgressCircular indeterminate />
        </div>
      ) : null}
      {state.error ? <Button variant="secondary" text="Retry" onClick={() => void state.inspect()} /> : null}
      {state.inspection ? (
        <ConfigurationOptions
          configurations={options}
          selected={selected}
          onSelect={(option) => setSelectedId(option.configurationId)}
        />
      ) : null}
    </ActionModal>
  );
}
