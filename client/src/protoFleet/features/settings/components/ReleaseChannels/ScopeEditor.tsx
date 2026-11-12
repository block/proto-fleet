import { type ReactElement, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";

import ContainerSelectionModal from "./ContainerSelectionModal";
import { isScopeEmpty, scopeSummary, scopeValidationErrors } from "./scopeUtils";
import {
  type PreviewReleaseChannelScopeResponse,
  type ReleaseChannelScope,
  ReleaseChannelScopeSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useContainers } from "@/protoFleet/api/useContainers";
import TargetSelectButton, { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";
import {
  BuildingSelectionModal,
  GroupSelectionModal,
  MinerSelectionModal,
  RackSelectionModal,
  SiteSelectionModal,
} from "@/protoFleet/components/TargetSelectionModal";

import { useHasPermission } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";

const PREVIEW_DEBOUNCE_MS = 300;

type SelectionKind = "site" | "building" | "rack" | "group" | "miner" | "container";

const toStrings = (ids: bigint[]): string[] => ids.map((id) => id.toString());
const toBigInts = (ids: string[]): bigint[] => ids.map((id) => BigInt(id));

interface PreviewRequest {
  scope: ReleaseChannelScope;
  load: (scope: ReleaseChannelScope, signal?: AbortSignal) => Promise<PreviewReleaseChannelScopeResponse>;
  attempt: number;
}

interface PreviewSnapshot {
  request: PreviewRequest;
  value: PreviewReleaseChannelScopeResponse;
}

interface ScopeEditorProps {
  scope: ReleaseChannelScope;
  onChange: (scope: ReleaseChannelScope) => void;
  // Resolves the scope live: miners per model and overlapping channels.
  previewScope: (scope: ReleaseChannelScope, signal?: AbortSignal) => Promise<PreviewReleaseChannelScopeResponse>;
  onPreview?: (preview: PreviewReleaseChannelScopeResponse | null) => void;
  editingExistingChannel?: boolean;
  disabled?: boolean;
}

// The "Applies to" section: one selector per placement level, opening the
// shared target selection modals and overlap validation. Creation also shows
// a live readout of what the scope resolves to.
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
  const canReadFleet = useHasPermission("fleet:read");
  // ListContainers returns a container only to callers holding all three.
  const canReadContainers = canReadSites && canReadFleet && canReadMiners;
  const canSelectTargets = canReadSites || canReadRacks || canReadMiners;
  const [openModal, setOpenModal] = useState<SelectionKind | null>(null);
  // Miners and containers share deviceIdentifiers; the catalog tells them
  // apart. It loads with the editor so a selection made later is classified
  // at once.
  const containerCatalog = useContainers([], false, canReadContainers);
  const containerIds = useMemo(
    () => new Set(containerCatalog.containers.map((container) => container.deviceIdentifier)),
    [containerCatalog.containers],
  );
  // Until the catalog arrives, saved identifiers cannot be split. Hold both
  // counts back, so a container is never shown as a miner first.
  const classifying = containerCatalog.loading && scope.deviceIdentifiers.length > 0;
  // Identifiers outside the readable catalog stay with the miners, as before.
  const minerIds = scope.deviceIdentifiers.filter((id) => !containerIds.has(id));
  const countContainers = (target: ReleaseChannelScope) =>
    target.deviceIdentifiers.filter((id) => containerIds.has(id)).length;
  const summarize = (target: ReleaseChannelScope) =>
    classifying ? "selection" : scopeSummary(target, countContainers(target));
  // The identifiers handed to the open miner picker. Its save replaces only
  // these, so containers survive even if the catalog changes while it is open.
  const [minerPickerIds, setMinerPickerIds] = useState<string[]>([]);
  const selectionButtons = [
    ["site", "Sites", scope.siteIds.length, canReadSites],
    ["building", "Buildings", scope.buildingIds.length, canReadSites],
    ["rack", "Racks", scope.rackIds.length, canReadRacks],
    ["group", "Groups", scope.groupIds.length, canReadRacks],
    ["miner", "Miners", minerIds.length, canReadMiners],
    ["container", "Containers", scope.deviceIdentifiers.length - minerIds.length, canReadContainers],
  ] as const;
  const [previewAttempt, setPreviewAttempt] = useState(0);
  const request = useMemo(
    () => ({ scope, load: previewScope, attempt: previewAttempt }),
    [scope, previewScope, previewAttempt],
  );
  const [lastPreview, setLastPreview] = useState<PreviewSnapshot | null>(null);
  const [previewError, setPreviewError] = useState<{ request: PreviewRequest; message: string } | null>(null);
  const validationErrors = scopeValidationErrors(scope);
  const previewMatchesScope = lastPreview?.request === request;
  const currentError = previewError?.request === request ? previewError.message : null;
  const preview = previewMatchesScope && !currentError && validationErrors.length === 0 ? lastPreview.value : null;

  const update = (patch: Partial<ReleaseChannelScope>) =>
    onChange(create(ReleaseChannelScopeSchema, { ...scope, ...patch }));

  // Debounced so a burst of selections resolves once; the latest request
  // wins. Invalidate the parent's conflict result immediately; old successful
  // results remain labeled as previous context, never as the changed scope.
  useEffect(() => {
    let cancelled = false;
    let controller: AbortController | undefined;
    onPreview?.(null);
    if (isScopeEmpty(request.scope) || scopeValidationErrors(request.scope).length > 0) return;
    const timer = setTimeout(() => {
      controller = new AbortController();
      request
        .load(request.scope, controller.signal)
        .then((result) => {
          if (cancelled) return;
          setLastPreview({ request, value: result });
          setPreviewError(null);
          onPreview?.(result);
        })
        .catch((error: unknown) => {
          if (cancelled) return;
          setPreviewError({
            request,
            message: error instanceof Error && error.message ? error.message : "Couldn't resolve the selection",
          });
          onPreview?.(null);
        });
    }, PREVIEW_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
      controller?.abort();
    };
    // onPreview is a notification callback; re-resolving when the parent
    // re-renders with a new function identity would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [request]);

  return (
    <div className="flex flex-col gap-3" data-testid="scope-editor">
      <div className="grid">
        {selectionButtons.map(([kind, label, count, permitted]) => {
          if (!permitted) return null;
          const pending = classifying && (kind === "miner" || kind === "container");
          return (
            <TargetSelectButton
              key={kind}
              label={label}
              value={pending ? "Loading…" : getTargetButtonLabel(count, kind)}
              disabled={disabled || pending}
              onClick={() => {
                if (kind === "miner") setMinerPickerIds(minerIds);
                setOpenModal(kind);
              }}
            />
          );
        })}
      </div>

      <ScopePreview
        scope={scope}
        summarize={summarize}
        preview={classifying ? null : preview}
        error={currentError}
        validationErrors={validationErrors}
        previousPreview={preview || classifying ? null : lastPreview}
        editingExistingChannel={editingExistingChannel}
        canSelectTargets={canSelectTargets}
        retryDisabled={disabled}
        onRetry={() => {
          onPreview?.(null);
          setPreviewAttempt((attempt) => attempt + 1);
        }}
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
          selectedMinerIds={minerPickerIds}
          filterConfig={canReadRacks ? undefined : { showRackFilter: false, showGroupFilter: false }}
          onDismiss={() => setOpenModal(null)}
          onSave={(selection) => {
            const replaced = new Set(minerPickerIds);
            const kept = scope.deviceIdentifiers.filter((id) => !replaced.has(id));
            update({ deviceIdentifiers: [...new Set([...kept, ...selection.selectedMinerIds])] });
            setOpenModal(null);
          }}
        />
      ) : null}
      {canReadContainers && openModal === "container" ? (
        <ContainerSelectionModal
          containers={containerCatalog.containers}
          loading={containerCatalog.loading}
          hasLoadError={containerCatalog.error !== undefined}
          selectedIdentifiers={scope.deviceIdentifiers}
          onDismiss={() => setOpenModal(null)}
          onSave={(identifiers) => {
            update({ deviceIdentifiers: [...minerIds, ...identifiers] });
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
  summarize,
  preview,
  error,
  validationErrors,
  previousPreview,
  editingExistingChannel,
  canSelectTargets,
  retryDisabled,
  onRetry,
}: {
  scope: ReleaseChannelScope;
  summarize: (scope: ReleaseChannelScope) => string;
  preview: PreviewReleaseChannelScopeResponse | null;
  error: string | null;
  validationErrors: string[];
  previousPreview: PreviewSnapshot | null;
  editingExistingChannel: boolean;
  canSelectTargets: boolean;
  retryDisabled: boolean;
  onRetry: () => void;
}): ReactElement | null => {
  if (isScopeEmpty(scope)) {
    return (
      <p className="text-200 text-text-primary-50" data-testid="scope-preview">
        {canSelectTargets
          ? "Select sites, buildings, racks, groups, or individual miners and containers. Each can belong to one release channel."
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
          <>
            <p role="alert" className="text-intent-critical-fill">
              {error}
            </p>
            {!editingExistingChannel ? (
              <p className="text-text-primary-50">
                Resolve this selection before creating the channel, or clear it to create an empty channel.
              </p>
            ) : null}
            <Button
              text="Retry preview"
              variant={variants.secondary}
              size={sizes.compact}
              onClick={onRetry}
              disabled={retryDisabled}
            />
          </>
        ) : (
          <p className="text-text-primary-50">
            Resolving {summarize(scope)}…
            {!editingExistingChannel ? " Wait for the preview before creating the channel." : ""}
          </p>
        )}
        {previousPreview && !editingExistingChannel ? (
          <p className="text-text-primary-50">
            Last valid preview: {summarize(previousPreview.request.scope)} · covers{" "}
            {previousPreview.value.minerCount.toLocaleString()}{" "}
            {previousPreview.value.minerCount === 1 ? "miner" : "miners"}.
          </p>
        ) : null}
      </div>
    );
  }
  if (editingExistingChannel && preview.conflicts.length === 0) return null;
  const models = preview.models.map((m) => `${m.minerCount.toLocaleString()} ${m.model || "unknown model"}`).join(", ");
  return (
    <div className="flex flex-col gap-1 text-200" data-testid="scope-preview">
      {!editingExistingChannel ? (
        <span className="text-text-primary">
          {summarize(scope)} · covers {preview.minerCount.toLocaleString()}{" "}
          {preview.minerCount === 1 ? "miner" : "miners"}
          {models ? ` (${models})` : ""}
        </span>
      ) : null}
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
