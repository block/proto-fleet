import { type ReactElement, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { isScopeEmpty, scopeSummary } from "./scopeUtils";
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

const PREVIEW_DEBOUNCE_MS = 300;

type SelectionKind = "site" | "building" | "rack" | "group" | "miner";

const toStrings = (ids: bigint[]): string[] => ids.map((id) => id.toString());
const toBigInts = (ids: string[]): bigint[] => ids.map((id) => BigInt(id));

interface ScopeEditorProps {
  scope: ReleaseChannelScope;
  onChange: (scope: ReleaseChannelScope) => void;
  // Resolves the scope live: miners per model and overlapping channels.
  previewScope: (scope: ReleaseChannelScope) => Promise<PreviewReleaseChannelScopeResponse>;
  onPreview?: (preview: PreviewReleaseChannelScopeResponse | null) => void;
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
  disabled = false,
}: ScopeEditorProps): ReactElement => {
  const [openModal, setOpenModal] = useState<SelectionKind | null>(null);
  const [preview, setPreview] = useState<PreviewReleaseChannelScopeResponse | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);

  const update = (patch: Partial<ReleaseChannelScope>) =>
    onChange(create(ReleaseChannelScopeSchema, { ...scope, ...patch }));

  // Debounced so a burst of selections resolves once; the latest request
  // wins. An empty scope clears the preview on the same schedule.
  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(() => {
      if (isScopeEmpty(scope)) {
        setPreview(null);
        setPreviewError(null);
        onPreview?.(null);
        return;
      }
      previewScope(scope)
        .then((result) => {
          if (cancelled) return;
          setPreview(result);
          setPreviewError(null);
          onPreview?.(result);
        })
        .catch((error: unknown) => {
          if (cancelled) return;
          setPreview(null);
          setPreviewError(error instanceof Error ? error.message : "Couldn't resolve the selection");
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
        <TargetSelectButton
          label="Sites"
          value={getTargetButtonLabel(scope.siteIds.length, "site")}
          disabled={disabled}
          onClick={() => setOpenModal("site")}
        />
        <TargetSelectButton
          label="Buildings"
          value={getTargetButtonLabel(scope.buildingIds.length, "building")}
          disabled={disabled}
          onClick={() => setOpenModal("building")}
        />
        <TargetSelectButton
          label="Racks"
          value={getTargetButtonLabel(scope.rackIds.length, "rack")}
          disabled={disabled}
          onClick={() => setOpenModal("rack")}
        />
        <TargetSelectButton
          label="Groups"
          value={getTargetButtonLabel(scope.groupIds.length, "group")}
          disabled={disabled}
          onClick={() => setOpenModal("group")}
        />
        <TargetSelectButton
          label="Miners"
          value={getTargetButtonLabel(scope.deviceIdentifiers.length, "miner")}
          disabled={disabled}
          onClick={() => setOpenModal("miner")}
        />
      </div>

      <ScopePreview scope={scope} preview={preview} error={previewError} />

      <SiteSelectionModal
        open={openModal === "site"}
        selectedSiteIds={toStrings(scope.siteIds)}
        onDismiss={() => setOpenModal(null)}
        onSave={(selection) => {
          update({ siteIds: toBigInts(selection.siteIds) });
          setOpenModal(null);
        }}
      />
      <BuildingSelectionModal
        open={openModal === "building"}
        selectedBuildingIds={toStrings(scope.buildingIds)}
        onDismiss={() => setOpenModal(null)}
        onSave={(buildingIds) => {
          update({ buildingIds: toBigInts(buildingIds) });
          setOpenModal(null);
        }}
      />
      <RackSelectionModal
        open={openModal === "rack"}
        selectedRackIds={toStrings(scope.rackIds)}
        onDismiss={() => setOpenModal(null)}
        onSave={(rackIds) => {
          update({ rackIds: toBigInts(rackIds) });
          setOpenModal(null);
        }}
      />
      <GroupSelectionModal
        open={openModal === "group"}
        selectedGroupIds={toStrings(scope.groupIds)}
        onDismiss={() => setOpenModal(null)}
        onSave={(groupIds) => {
          update({ groupIds: toBigInts(groupIds) });
          setOpenModal(null);
        }}
      />
      {openModal === "miner" ? (
        <MinerSelectionModal
          open
          selectedMinerIds={scope.deviceIdentifiers}
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

// What the scope resolves to right now, and any channels it would overlap
// (which blocks saving).
const ScopePreview = ({
  scope,
  preview,
  error,
}: {
  scope: ReleaseChannelScope;
  preview: PreviewReleaseChannelScopeResponse | null;
  error: string | null;
}): ReactElement => {
  if (isScopeEmpty(scope)) {
    return (
      <p className="text-200 text-text-primary-50" data-testid="scope-preview">
        Select sites, buildings, racks, groups or individual miners. A miner can belong to one release channel.
      </p>
    );
  }
  if (error) {
    return (
      <p className="text-200 text-text-critical" data-testid="scope-preview">
        {error}
      </p>
    );
  }
  if (!preview) {
    return (
      <p className="text-200 text-text-primary-50" data-testid="scope-preview">
        Resolving {scopeSummary(scope)}…
      </p>
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
          . Remove those miners from one of the channels before saving.
        </span>
      ) : null}
    </div>
  );
};

export default ScopeEditor;
