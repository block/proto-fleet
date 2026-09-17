import { type ReactElement, useEffect, useState } from "react";
import { create, equals } from "@bufbuild/protobuf";

import { isScopeEmpty, scopeSummary, scopeValidationErrors } from "./scopeUtils";
import {
  type PreviewReleaseChannelScopeResponse,
  type ReleaseChannelScope,
  ReleaseChannelScopeSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import TargetSelectButton, { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";
import {
  BuildingSelectionModal,
  GroupSelectionModal,
  MinerSelectionModal,
  RackSelectionModal,
  SiteSelectionModal,
} from "@/protoFleet/components/TargetSelectionModal";

import { useHasPermission } from "@/protoFleet/store";

const PREVIEW_DEBOUNCE_MS = 300;

type SelectionKind = "site" | "building" | "rack" | "group" | "miner";

const toStrings = (ids: bigint[]): string[] => ids.map((id) => id.toString());
const toBigInts = (ids: string[]): bigint[] => ids.map((id) => BigInt(id));

interface PreviewSnapshot {
  scope: ReleaseChannelScope;
  value: PreviewReleaseChannelScopeResponse;
}

interface ScopeEditorProps {
  scope: ReleaseChannelScope;
  onChange: (scope: ReleaseChannelScope) => void;
  // Resolves the scope live: miners per model and overlapping channels.
  previewScope: (scope: ReleaseChannelScope) => Promise<PreviewReleaseChannelScopeResponse>;
  onPreview?: (preview: PreviewReleaseChannelScopeResponse | null) => void;
  editingExistingChannel?: boolean;
  disabled?: boolean;
}

// The "Applies to" section: one selector per placement level, opening the
// shared target selection modals, with a live readout of what the scope
// resolves to and which channels it would overlap.
const ScopeEditor = ({
  scope,
  onChange,
  previewScope,
  onPreview,
  editingExistingChannel = false,
  disabled = false,
}: ScopeEditorProps): ReactElement => {
  const canReadSites = useHasPermission("site:read");
  const canReadRacks = useHasPermission("rack:read");
  const canReadMiners = useHasPermission("miner:read");
  const canSelectTargets = canReadSites || canReadRacks || canReadMiners;
  const [openModal, setOpenModal] = useState<SelectionKind | null>(null);
  const [lastPreview, setLastPreview] = useState<PreviewSnapshot | null>(null);
  const [previewError, setPreviewError] = useState<{ scope: ReleaseChannelScope; message: string } | null>(null);
  const validationErrors = scopeValidationErrors(scope);
  const previewMatchesScope = lastPreview !== null && equals(ReleaseChannelScopeSchema, lastPreview.scope, scope);
  const currentError =
    previewError && equals(ReleaseChannelScopeSchema, previewError.scope, scope) ? previewError.message : null;
  const preview = previewMatchesScope && !currentError && validationErrors.length === 0 ? lastPreview.value : null;

  const update = (patch: Partial<ReleaseChannelScope>) =>
    onChange(create(ReleaseChannelScopeSchema, { ...scope, ...patch }));

  // Debounced so a burst of selections resolves once; the latest request
  // wins. Invalidate the parent's conflict result immediately; old successful
  // results remain labeled as previous context, never as the changed scope.
  useEffect(() => {
    let cancelled = false;
    onPreview?.(null);
    if (isScopeEmpty(scope) || scopeValidationErrors(scope).length > 0) return;
    const timer = setTimeout(() => {
      previewScope(scope)
        .then((result) => {
          if (cancelled) return;
          setLastPreview({ scope, value: result });
          setPreviewError(null);
          onPreview?.(result);
        })
        .catch((error: unknown) => {
          if (cancelled) return;
          setPreviewError({
            scope,
            message: error instanceof Error ? error.message : "Couldn't resolve the selection",
          });
          onPreview?.(null);
        });
    }, PREVIEW_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
    // onPreview is a notification callback; re-resolving when the parent
    // re-renders with a new function identity would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, previewScope]);

  return (
    <div className="flex flex-col gap-3" data-testid="scope-editor">
      <div className="grid">
        {canReadSites ? (
          <TargetSelectButton
            label="Sites"
            value={getTargetButtonLabel(scope.siteIds.length, "site")}
            disabled={disabled}
            onClick={() => setOpenModal("site")}
          />
        ) : null}
        {canReadSites ? (
          <TargetSelectButton
            label="Buildings"
            value={getTargetButtonLabel(scope.buildingIds.length, "building")}
            disabled={disabled}
            onClick={() => setOpenModal("building")}
          />
        ) : null}
        {canReadRacks ? (
          <TargetSelectButton
            label="Racks"
            value={getTargetButtonLabel(scope.rackIds.length, "rack")}
            disabled={disabled}
            onClick={() => setOpenModal("rack")}
          />
        ) : null}
        {canReadRacks ? (
          <TargetSelectButton
            label="Groups"
            value={getTargetButtonLabel(scope.groupIds.length, "group")}
            disabled={disabled}
            onClick={() => setOpenModal("group")}
          />
        ) : null}
        {canReadMiners ? (
          <TargetSelectButton
            label="Miners"
            value={getTargetButtonLabel(scope.deviceIdentifiers.length, "miner")}
            disabled={disabled}
            onClick={() => setOpenModal("miner")}
          />
        ) : null}
      </div>

      <ScopePreview
        scope={scope}
        preview={preview}
        error={currentError}
        validationErrors={validationErrors}
        previousPreview={preview ? null : lastPreview}
        editingExistingChannel={editingExistingChannel}
        canSelectTargets={canSelectTargets}
      />

      {canReadSites && openModal === "site" ? (
        <SiteSelectionModal
          open
          selectedSiteIds={toStrings(scope.siteIds)}
          onDismiss={() => setOpenModal(null)}
          onSave={(selection) => {
            update({ siteIds: toBigInts(selection.siteIds) });
            setOpenModal(null);
          }}
        />
      ) : null}
      {canReadSites && openModal === "building" ? (
        <BuildingSelectionModal
          open
          selectedBuildingIds={toStrings(scope.buildingIds)}
          onDismiss={() => setOpenModal(null)}
          onSave={(buildingIds) => {
            update({ buildingIds: toBigInts(buildingIds) });
            setOpenModal(null);
          }}
        />
      ) : null}
      {canReadRacks && openModal === "rack" ? (
        <RackSelectionModal
          open
          selectedRackIds={toStrings(scope.rackIds)}
          onDismiss={() => setOpenModal(null)}
          onSave={(rackIds) => {
            update({ rackIds: toBigInts(rackIds) });
            setOpenModal(null);
          }}
        />
      ) : null}
      {canReadRacks && openModal === "group" ? (
        <GroupSelectionModal
          open
          selectedGroupIds={toStrings(scope.groupIds)}
          onDismiss={() => setOpenModal(null)}
          onSave={(groupIds) => {
            update({ groupIds: toBigInts(groupIds) });
            setOpenModal(null);
          }}
        />
      ) : null}
      {canReadMiners && openModal === "miner" ? (
        <MinerSelectionModal
          open
          selectedMinerIds={scope.deviceIdentifiers}
          filterConfig={canReadRacks ? undefined : { showRackFilter: false, showGroupFilter: false }}
          onDismiss={() => setOpenModal(null)}
          onSave={(selection) => {
            update({ deviceIdentifiers: selection.selectedMinerIds });
            setOpenModal(null);
          }}
        />
      ) : null}
    </div>
  );
};

// What the scope resolves to right now, and any channels it would overlap.
const ScopePreview = ({
  scope,
  preview,
  error,
  validationErrors,
  previousPreview,
  editingExistingChannel,
  canSelectTargets,
}: {
  scope: ReleaseChannelScope;
  preview: PreviewReleaseChannelScopeResponse | null;
  error: string | null;
  validationErrors: string[];
  previousPreview: PreviewSnapshot | null;
  editingExistingChannel: boolean;
  canSelectTargets: boolean;
}): ReactElement => {
  if (isScopeEmpty(scope)) {
    return (
      <p className="text-200 text-text-primary-50" data-testid="scope-preview">
        {canSelectTargets
          ? "Select sites, buildings, racks, groups or individual miners. A miner can belong to one release channel."
          : "You can save an empty channel. Selecting targets requires permission to view sites, racks or miners."}
      </p>
    );
  }
  if (validationErrors.length > 0 || error || !preview) {
    return (
      <div className="flex flex-col gap-1 text-200" data-testid="scope-preview">
        {validationErrors.length > 0 ? (
          <div role="alert" className="text-intent-critical-fill">
            {validationErrors.map((message) => (
              <p key={message}>{message}</p>
            ))}
          </div>
        ) : error ? (
          <p className="text-intent-critical-fill">{error}</p>
        ) : (
          <p className="text-text-primary-50">Resolving {scopeSummary(scope)}…</p>
        )}
        {previousPreview ? (
          <p className="text-text-primary-50">
            Last valid preview: {scopeSummary(previousPreview.scope)} · covers{" "}
            {previousPreview.value.minerCount.toLocaleString()}{" "}
            {previousPreview.value.minerCount === 1 ? "miner" : "miners"}.
          </p>
        ) : null}
      </div>
    );
  }
  const models = preview.models.map((m) => `${m.minerCount.toLocaleString()} ${m.model || "unknown model"}`).join(", ");
  return (
    <div className="flex flex-col gap-1 text-200" data-testid="scope-preview">
      <span className="text-text-primary">
        {scopeSummary(scope)} · covers {preview.minerCount.toLocaleString()}{" "}
        {preview.minerCount === 1 ? "miner" : "miners"}
        {models ? ` (${models})` : ""}
      </span>
      {preview.conflicts.length > 0 ? (
        <span className="text-text-critical" data-testid="scope-conflicts">
          Overlaps{" "}
          {preview.conflicts
            .map(
              (c) => `${c.channelName} (${c.minerCount.toLocaleString()} ${c.minerCount === 1 ? "miner" : "miners"})`,
            )
            .join(", ")}
          .{" "}
          {editingExistingChannel
            ? "Existing overlaps can remain. Changes that add new overlaps cannot be saved."
            : "Remove those miners from one of the channels before saving."}
        </span>
      ) : null}
    </div>
  );
};

export default ScopeEditor;
