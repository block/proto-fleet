import { useState } from "react";
import ConfigurationEditor from "./ConfigurationEditor";
import { usePoolAssignment } from "./usePoolAssignment";
import {
  type AssignmentPool,
  changesWorkerNamePools,
  currentWorkerName,
  INVALID_WORKER_NAME_MESSAGE,
  isValidWorkerName,
  type LocalPoolConfiguration,
  MINING_GROUP_LIMIT_MESSAGE,
  type PoolAssignmentCapacity,
  type PoolAssignmentPlan,
  type PoolAssignmentService,
  type PoolAssignmentTarget,
  type SavedPoolSet,
  type WorkerNameSource,
} from "@/protoOS/api/adapters/poolAssignment";
import { validateMiningGroupPoolURL } from "@/protoOS/api/adapters/poolURL";
import PoolHashboards from "@/protoOS/features/settings/components/MiningPools/PoolHashboards";
import { Alert } from "@/shared/assets/icons";
import { ActionModal } from "@/shared/components/ActionModal";
import AffectedEquipment from "@/shared/components/AffectedEquipment";
import {
  bulkRenameModes,
  type BulkRenamePreviewMiner,
  bulkRenamePropertyIds,
  bulkRenameSeparatorIds,
  createDefaultBulkRenamePreferences,
  getEnabledBulkRenameProperties,
  shouldWarnAboutBulkRenameDuplicates,
} from "@/shared/components/BulkRename/bulkRenameDefinitions";
import BulkRenameDialogs from "@/shared/components/BulkRename/BulkRenameDialogs";
import { evaluateBulkRenamePreviewName } from "@/shared/components/BulkRename/bulkRenamePreview";
import BulkWorkerNameEditor from "@/shared/components/BulkRename/BulkWorkerNameEditor";
import Button from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import {
  type ConfigurationChange,
  ConfigurationChanges,
} from "@/shared/components/PoolConfigurations/ConfigurationChanges";
import { ConfigurationPools } from "@/shared/components/PoolConfigurations/ConfigurationDetails";
import { PoolConfigurationPicker } from "@/shared/components/PoolConfigurations/PoolConfigurationPicker";
import { poolSlotLabel } from "@/shared/components/PoolConfigurations/types";
import { WorkerNameReviewDialog } from "@/shared/components/PoolConfigurations/WorkerNameReviewDialog";
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

const defaultWorkerNamePreferences = createDefaultBulkRenamePreferences(bulkRenameModes.worker);
const boardWorkerNameSeparators = [
  bulkRenameSeparatorIds.dash,
  bulkRenameSeparatorIds.underscore,
  bulkRenameSeparatorIds.none,
] as const;
const boardOrder = new Intl.Collator(undefined, { numeric: true });
const boardPropertyLabels = {
  [bulkRenamePropertyIds.fixedMinerName]: "Hashboard name",
  [bulkRenamePropertyIds.fixedModel]: "ASIC model",
  [bulkRenamePropertyIds.qualifierRack]: "Tank",
  [bulkRenamePropertyIds.qualifierRackPosition]: "Module and board position",
};
const asNameProperty = (value = "") => value.trim().replace(/\s*·\s*|\s+/gu, "-");

function workerNamePreviewBoards(
  sources: WorkerNameSource[],
): Array<BulkRenamePreviewMiner & { pools: SavedPoolSet["pools"] }> {
  return sources
    .flatMap(({ boards, configuration }) => {
      const currentName = currentWorkerName(configuration.pools);
      return boards.map((board) => ({ ...board, currentName, pools: configuration.pools }));
    })
    .sort(
      (left, right) =>
        boardOrder.compare(left.location ?? "", right.location ?? "") || left.mac.localeCompare(right.mac),
    )
    .map((board, counterIndex) => ({
      counterIndex,
      deviceIdentifier: board.mac,
      currentName: board.currentName,
      storedName: board.currentName,
      macAddress: board.mac,
      serialNumber: asNameProperty(board.serialNumber),
      minerName: asNameProperty(board.location),
      model: asNameProperty(board.model),
      manufacturer: "Proto",
      workerName: board.currentName,
      rackLabel: asNameProperty(board.tank),
      rackPosition: board.position ?? "",
      pools: board.pools,
    }));
}

export default function PoolAssignmentDialog({
  service,
  controllerId,
  target,
  openStatus = false,
  workerNames = false,
  onDismiss,
  configuration,
  configurationCapacity,
  onConfigurationSaved,
  onAssignmentSaved,
  workerNameUpdateOwner,
}: {
  onConfigurationSaved?: () => void;
  onAssignmentSaved?: () => void;
  workerNameUpdateOwner?: string;
  service: PoolAssignmentService;
  controllerId: string;
  target: PoolAssignmentTarget;
  openStatus?: boolean;
  workerNames?: boolean;
  onDismiss: () => void;
} & (
  | { configuration?: undefined; configurationCapacity?: undefined }
  | { configuration: LocalPoolConfiguration | "new"; configurationCapacity: PoolAssignmentCapacity }
)) {
  const [creating, setCreating] = useState(Boolean(configuration));
  const [workerNameWarning, setWorkerNameWarning] = useState<"unchanged" | "duplicate" | "overwrite">();
  const [usernameSource, setUsernameSource] = useState<SavedPoolSet>();
  const [draft, setDraft] = useState<{ name: string; pools: AssignmentPool[] }>();
  const [selectedId, setSelectedId] = useState<string>();
  const original = configuration && configuration !== "new" ? configuration : undefined;
  const state = usePoolAssignment(service, controllerId, target, {
    openStatus,
    workerNames,
    onDismiss,
    workerNameUpdateOwner,
    onConfigurationSaved: () => {
      setDraft(undefined);
      setCreating(false);
      onConfigurationSaved?.();
    },
    onAssignmentSaved: (message, toastAlreadyShown) => {
      if (!toastAlreadyShown) pushToast({ message, status: STATUSES.success });
      onAssignmentSaved?.();
      onDismiss();
    },
  });
  const close = () => {
    if (!state.busy) onDismiss();
  };
  const attempt = state.attempt;
  const inspection = state.inspection;
  const capacity = inspection?.capacity ?? configurationCapacity;
  const noGroupSpace = capacity && capacity.groupsUsed >= capacity.maxGroups;
  const reviewingRemaining = state.plan && state.plan.id !== attempt?.id;
  if (state.updatingWorkerNames) return null;
  if (openStatus && !attempt) {
    return (
      <Modal
        open
        title="Pool assignment status"
        dialogLabel="Pool assignment status"
        manageFocus
        onDismiss={close}
        buttons={[{ text: "Close", variant: "secondary", onClick: close }]}
      >
        <p>There is no pool assignment to check.</p>
      </Modal>
    );
  }
  if (attempt && !reviewingRemaining && !(state.busy && attempt.configurationOnly)) {
    const settled = state.status && state.status.state !== "pending";
    const miningCheckRequired = attempt.restoreLegacyMining === true;
    const canReviewRemaining = Boolean(state.status?.remainingMacs?.length && attempt.destinationId);
    const unsubmitted = attempt.unsubmittedWorkerNameBoards;
    const title =
      unsubmitted !== undefined
        ? "Worker name update status"
        : attempt.configurationOnly
          ? "Pool configuration status"
          : "Pool assignment status";
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
        title={title}
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
        {!state.busy && unsubmitted ? (
          <p>
            Review the other {unsubmitted} selected hashboard{unsubmitted === 1 ? "" : "s"} again after this update
            finishes.
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
  if (state.workerNameReview) {
    const groups = state.workerNameReview.groups.map(({ assignment, workerName }) => {
      const sourcePools = assignment.originalPools ?? assignment.usernameSource?.pools ?? [];
      const before = currentWorkerName(sourcePools);
      const usernameChanges = assignment.pools.flatMap(({ username }, index) =>
        sourcePools[index]?.username === username
          ? []
          : [
              {
                field: `${poolSlotLabel(index)} username`,
                before: sourcePools[index]?.username ?? "",
                after: username,
              },
            ],
      );
      return {
        id: assignment.id,
        name: assignment.usernameSource?.name ?? assignment.name,
        changes: assignment.boards.map(({ mac, location }) => ({
          id: mac,
          label: location ?? mac,
          before,
          after: workerName,
        })),
        details: usernameChanges.length ? (
          <div className="mt-3">
            <ConfigurationChanges changes={usernameChanges} />
          </div>
        ) : undefined,
      };
    });
    const boardCount = groups.reduce((count, group) => count + group.changes.length, 0);
    const split = state.workerNameReview.groups.some(({ assignment }) => assignment.usernameSource);
    return (
      <WorkerNameReviewDialog
        groups={groups}
        summary={
          <>
            <p>
              Verify each complete pool username before saving {boardCount}{" "}
              {boardCount === 1 ? "worker name" : "worker names"}. Pool changes may briefly interrupt mining.
            </p>
            {split ? (
              <p>
                This change uses {state.workerNameReview.capacity.groupsUsed} of{" "}
                {state.workerNameReview.capacity.maxGroups} mining groups. New groups can open separate pool
                connections.
              </p>
            ) : null}
          </>
        }
        busy={state.busy}
        error={state.error}
        onDismiss={state.backFromWorkerNames}
        onSubmit={state.confirmWorkerNames}
      />
    );
  }
  if (state.plan) {
    const plan = state.plan;
    const hashboardCount = plan.target.macs.length;
    const hashboardLabel = hashboardCount === 1 ? "hashboard" : "hashboards";
    const editing = plan.editingRevision && hashboardCount ? original : undefined;
    const pools = plan.savedPoolSet?.pools ?? plan.pools;
    const movingBoards = new Set(plan.transfers.flatMap((transfer) => transfer.macs));
    const showTransfer = !plan.configurationOnly && movingBoards.size < hashboardCount;
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
        {editing || !plan.configurationOnly ? (
          <AffectedEquipment
            title="Affected hashboards"
            itemName="hashboards"
            items={plan.boards.map(({ mac, location }) => {
              const transfer = showTransfer
                ? movingBoards.has(mac)
                  ? "Moves to this group"
                  : "Already in this group"
                : undefined;
              return { id: mac, label: [location, mac, transfer].filter(Boolean).join(" · ") };
            })}
          />
        ) : null}
        {editing ? (
          <ConfigurationChanges changes={configurationChanges(editing, plan)} />
        ) : (
          <ConfigurationPools pools={pools} />
        )}
        {!plan.configurationOnly ? (
          <>
            <p>
              {plan.capacity.destinationBoards} of {plan.capacity.maxBoards} hashboard places will be used in this
              group.
            </p>
            {plan.usernameSource ? (
              <p>
                This change uses {plan.capacity.groupsUsed} of {plan.capacity.maxGroups} mining groups. A new group can
                open separate connections. Check your pool provider’s connection limits.
              </p>
            ) : null}
          </>
        ) : null}
      </ActionModal>
    );
  }
  if (workerNames) {
    const workerDraft = state.workerNameDraft;
    const preferences = workerDraft?.preferences ?? defaultWorkerNamePreferences;
    const sources = workerDraft?.sources ?? [];
    const previewBoards = workerNamePreviewBoards(sources);
    const previewRows = previewBoards.map((board) => ({
      deviceIdentifier: board.deviceIdentifier,
      currentName: board.currentName,
      pools: board.pools,
      newName: evaluateBulkRenamePreviewName(preferences, board, board.counterIndex),
    }));
    const hasProperties = getEnabledBulkRenameProperties(preferences).length > 0;
    const generatedNameError =
      hasProperties && previewRows.some(({ newName }) => !isValidWorkerName(newName))
        ? INVALID_WORKER_NAME_MESSAGE
        : undefined;
    const boardCount = target.macs.length;
    const showPreviewEllipsis = previewRows.length > 6;
    const loadingPreview = !workerDraft && !state.error;
    const feedback = state.error ?? generatedNameError;
    const hasCurrentNames = previewBoards.some(({ storedName }) => storedName !== "");
    const dismissWarning = () => setWorkerNameWarning(undefined);
    const reviewWorkerNames = () => {
      dismissWarning();
      void state.reviewWorkerNames({
        namesByMac: Object.fromEntries(previewRows.map(({ deviceIdentifier, newName }) => [deviceIdentifier, newName])),
        sources,
      });
    };
    const confirmOrReview = () => {
      if (hasCurrentNames) setWorkerNameWarning("overwrite");
      else reviewWorkerNames();
    };
    const handleApply = () => {
      if (
        !hasProperties ||
        previewRows.every(({ newName, pools }) => !newName || !changesWorkerNamePools(pools, newName))
      ) {
        setWorkerNameWarning("unchanged");
      } else if (
        new Set(previewRows.map(({ newName }) => newName)).size < previewRows.length ||
        shouldWarnAboutBulkRenameDuplicates(boardCount, preferences, previewBoards)
      ) {
        setWorkerNameWarning("duplicate");
      } else confirmOrReview();
    };
    return (
      <>
        <BulkWorkerNameEditor
          preferences={preferences}
          onPreferencesChange={(next) =>
            state.setWorkerNameDraft((current) => current && { ...current, preferences: next })
          }
          previewMiners={previewBoards}
          previewRows={showPreviewEllipsis ? [...previewRows.slice(0, 3), ...previewRows.slice(-3)] : previewRows}
          showPreviewEllipsis={showPreviewEllipsis}
          isLoadingPreview={loadingPreview}
          isBusy={state.busy}
          applyDisabled={!workerDraft || Boolean(generatedNameError)}
          applyLabel={`Apply to ${boardCount} ${boardCount === 1 ? "hashboard" : "hashboards"}`}
          propertyLabels={boardPropertyLabels}
          separatorIds={boardWorkerNameSeparators}
          onDismiss={close}
          onApply={handleApply}
          feedback={
            feedback ? (
              <>
                <Callout intent={intents.warning} prefixIcon={<Alert />} title={feedback} />
                {state.error ? (
                  <Button
                    variant="secondary"
                    className="self-start"
                    text="Reload configurations"
                    onClick={state.inspect}
                  />
                ) : null}
              </>
            ) : undefined
          }
        />
        <BulkRenameDialogs
          open
          showDuplicateNamesWarning={workerNameWarning === "duplicate"}
          showNoChangesWarning={workerNameWarning === "unchanged"}
          duplicateNamesDialogBody="Some hashboards may have duplicate worker names. Proceeding may impact accuracy in pool dashboards. Do you want to continue anyway?"
          noChangesDialogBody="You can continue to retain your existing worker names, or keep editing. Do you want to continue anyway?"
          onDismissDuplicateNames={dismissWarning}
          onContinueDuplicateNames={confirmOrReview}
          onDismissNoChanges={dismissWarning}
          onContinueNoChanges={close}
          {...(workerNameWarning === "overwrite"
            ? {
                showOverwriteWarning: true,
                overwriteDialogTitle: "Overwrite existing worker names?",
                overwriteDialogBody:
                  "This will replace existing worker names for the selected hashboards. The apply action will also update current pool settings to use the new worker names.",
                onDismissOverwriteWarning: dismissWarning,
                onContinueOverwriteWarning: reviewWorkerNames,
              }
            : { showOverwriteWarning: false })}
        />
      </>
    );
  }
  if ((creating || usernameSource) && capacity) {
    return (
      <ConfigurationEditor
        draft={draft}
        original={original}
        usernameSource={usernameSource}
        maxPools={capacity.maxPools}
        busy={state.busy}
        error={state.error}
        onDismiss={() => {
          if (configuration) close();
          else {
            setDraft(undefined);
            setCreating(false);
            setUsernameSource(undefined);
          }
        }}
        onSubmit={(name, pools) => {
          setDraft({ name, pools });
          if (usernameSource) void state.editUsernames(name, pools, usernameSource);
          else void state.create(name, pools, original);
        }}
      />
    );
  }

  const options = inspection?.savedPools.map((set) => ({ ...set, configurationId: set.id }));
  const currentSelectionId = selectedId ?? state.createdId ?? inspection?.currentPoolSetId;
  const selected = options?.find((option) => option.configurationId === currentSelectionId);
  const loadingConfigurations = !inspection && !state.error;
  const destinationFull = selected && capacity && selected.projectedBoards > capacity.maxBoards;
  let copyBlocked;
  if (capacity && selected) {
    if (target.macs.length > capacity.maxBoards) copyBlocked = "Select fewer hashboards to edit usernames.";
    else if (selected.pools.length > capacity.maxPools)
      copyBlocked = "This configuration exceeds the pool limit for a new group.";
    else if (selected.pools.some((pool) => validateMiningGroupPoolURL(pool.url)))
      copyBlocked = "A new mining group does not support one or more of these pool URLs.";
  }
  return (
    <ActionModal
      title="Edit pool"
      initialFocus="dialog"
      confirm="Review assignment"
      valid={Boolean(selected && !destinationFull && !state.error)}
      busy={state.busy}
      onDismiss={close}
      onSubmit={() => {
        if (selected) void state.reviewAssignment(selected.configurationId, selected.name);
      }}
    >
      <PoolConfigurationPicker
        selectionDescription={`${target.macs.length} ${target.macs.length === 1 ? "hashboard" : "hashboards"} selected.`}
        mixed={inspection?.mixedConfigurations}
        onCreate={() => {
          setSelectedId(undefined);
          setCreating(true);
        }}
        createDisabled={!inspection || state.busy || noGroupSpace}
        additionalAction={
          selected ? (
            <Button
              variant="secondary"
              text="Edit usernames"
              disabled={state.busy || Boolean(state.error || noGroupSpace || copyBlocked)}
              onClick={() => setUsernameSource(selected)}
            />
          ) : null
        }
        loading={loadingConfigurations}
        error={state.error}
        onRetry={() => void state.inspect()}
        configurations={options}
        selected={selected}
        onSelect={(option) => setSelectedId(option.configurationId)}
      />
      {capacity ? (
        <p>
          {capacity.groupsUsed} of {capacity.maxGroups} mining groups in use. Each group supports up to{" "}
          {capacity.maxBoards} hashboards and {capacity.maxPools} pools.
        </p>
      ) : null}
      {noGroupSpace ? <p role="status">{MINING_GROUP_LIMIT_MESSAGE}</p> : null}
      {destinationFull ? (
        <p role="status">
          This configuration would have {selected.projectedBoards} hashboards. Select fewer hashboards or choose another
          configuration.
        </p>
      ) : null}
      {copyBlocked && !noGroupSpace ? <p role="status">{copyBlocked}</p> : null}
      {(noGroupSpace || destinationFull || copyBlocked) && !state.error ? (
        <Button
          variant="secondary"
          className="self-start"
          text="Reload configurations"
          onClick={() => void state.inspect()}
        />
      ) : null}
    </ActionModal>
  );
}
