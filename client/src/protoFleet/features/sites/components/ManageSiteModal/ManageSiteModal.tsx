import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import ManageBuildingsModal from "../ManageBuildingsModal";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type Site } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type SiteFormValues } from "@/protoFleet/api/sites";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { formatSiteAddress } from "@/protoFleet/features/sites/formatAddress";
import { Ellipsis } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

export type ManageSiteModalMode = "create" | "edit";

// One building in the modal's working set. Seeded from the server fetch and
// mutated locally by the Manage buildings picker; persisted on Save.
interface BuildingEntry {
  buildingId: bigint;
  label: string;
  rackCount: bigint;
}

// Net membership change between the load-time snapshot and the working set,
// computed on Save and applied by the host via AssignBuildingsToSite.
export interface BuildingMembershipDelta {
  added: bigint[];
  removed: bigint[];
}

interface ManageSiteModalProps {
  open: boolean;
  mode: ManageSiteModalMode;
  draft: SiteFormValues;
  // In edit mode the parent has a Site row to drive the right-pane preview
  // header off; in create mode there is no row yet so the preview uses the
  // draft values directly.
  site?: Site;
  // Persisted at save time. In edit mode the delta is applied via
  // AssignBuildingsToSite; in create mode the host first creates the site
  // (the delta is empty since building management is gated until the site
  // exists). Returns whether the modal should close on success.
  onSave: (delta: BuildingMembershipDelta) => Promise<{ closeOnSuccess: boolean } | null>;
  // Opens SiteSettingsModal stacked on top to edit name / address / etc.
  onEditDetails: () => void;
  // Opens the cascade delete dialog (edit) or discards the pending create.
  // Mirrors ManageBuildingModal's header Delete CTA.
  onDeleteRequested: () => void;
  onDismiss: () => void;
  saving?: boolean;
  // Refresh signal — bumped by the host whenever the building cache changes
  // (e.g. a building deleted from the settings table) so the modal's local
  // list re-fetches without bouncing through unmount/remount.
  buildingsRefreshKey?: number;
}

// Row layout mirrors MinerRow from ManageRackModal: name + secondary line
// stack on the left, a kebab menu on the right. Buildings have no placement
// state, so there's no leading icon column or row selection — the only row
// action is "Remove building", which drops it from the site's working set
// (the building itself is not deleted).
const BuildingRow = ({
  buildingId,
  label,
  rackCount,
  saving,
  onRemove,
}: {
  buildingId: bigint;
  label: string;
  rackCount: bigint;
  saving: boolean;
  onRemove: (buildingId: bigint) => void;
}) => {
  const [showMenu, setShowMenu] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const handleRemove = useCallback(() => {
    setShowMenu(false);
    onRemove(buildingId);
  }, [buildingId, onRemove]);

  useEscapeDismiss(showMenu ? () => setShowMenu(false) : undefined);

  return (
    <div
      className="flex items-center px-3 py-3"
      data-testid={`manage-site-modal-building-row-${buildingId.toString()}`}
    >
      <div className="min-w-0 flex-1">
        <div className="truncate text-300 text-text-primary">{label || "(unnamed building)"}</div>
        <div className="truncate text-300 text-text-primary-50">
          {rackCount.toString()} {rackCount === 1n ? "rack" : "racks"}
        </div>
      </div>

      <div className="relative shrink-0" ref={menuRef}>
        <button
          type="button"
          aria-label="Building options"
          className="flex h-8 w-8 items-center justify-center rounded-lg text-text-primary-70 hover:cursor-pointer"
          disabled={saving}
          onClick={(e) => {
            e.stopPropagation();
            setShowMenu((prev) => !prev);
          }}
          data-testid={`manage-site-modal-building-menu-${buildingId.toString()}`}
        >
          <Ellipsis width="w-4" />
        </button>
        {showMenu ? (
          <>
            <div
              className="fixed inset-0 z-20"
              onClick={(e) => {
                e.stopPropagation();
                setShowMenu(false);
              }}
            />
            <div className="absolute top-full right-0 z-30 mt-1 w-44 rounded-xl border border-border-5 bg-surface-elevated-base py-1 shadow-300">
              <button
                type="button"
                className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-2"
                onClick={(e) => {
                  e.stopPropagation();
                  handleRemove();
                }}
                data-testid={`manage-site-modal-remove-building-${buildingId.toString()}`}
              >
                Remove building
              </button>
            </div>
          </>
        ) : null}
      </div>
    </div>
  );
};

const ManageSiteModal = ({
  open,
  mode,
  draft,
  site,
  onSave,
  onEditDetails,
  onDeleteRequested,
  onDismiss,
  saving = false,
  buildingsRefreshKey = 0,
}: ManageSiteModalProps) => {
  const { listBuildingsBySite } = useBuildings();
  // undefined = loading; [] = loaded-empty. Working set the operator edits
  // via the picker before Save.
  const [entries, setEntries] = useState<BuildingEntry[] | undefined>(undefined);
  const [showManageBuildings, setShowManageBuildings] = useState(false);

  // Snapshot of the building ids present at load time so Save can diff the
  // working set into add / remove buckets for AssignBuildingsToSite.
  const initialIdsRef = useRef<Set<string>>(new Set());

  // Only fetch when edit mode has a persisted site; create mode renders an
  // empty working set until the first Save lands a row. Skipping the effect
  // for the no-fetch branches keeps the setState-in-effect lint clean.
  const shouldFetchBuildings = open && mode === "edit" && site !== undefined;
  const fetchSiteId = shouldFetchBuildings ? site.id : undefined;
  useEffect(() => {
    if (!shouldFetchBuildings || fetchSiteId === undefined) return;
    const controller = new AbortController();
    void listBuildingsBySite({
      siteId: fetchSiteId,
      signal: controller.signal,
      onSuccess: (rows: BuildingWithCounts[]) => {
        const seeded: BuildingEntry[] = rows
          .filter((r) => r.building)
          .map((r) => ({
            buildingId: r.building?.id ?? 0n,
            label: r.building?.name ?? "(unnamed)",
            rackCount: r.rackCount,
          }));
        setEntries(seeded);
        initialIdsRef.current = new Set(seeded.map((e) => e.buildingId.toString()));
      },
      onError: () => {
        setEntries([]);
        initialIdsRef.current = new Set();
      },
    });
    return () => controller.abort();
  }, [shouldFetchBuildings, fetchSiteId, listBuildingsBySite, buildingsRefreshKey]);

  // Create mode never fetches, so its working set is empty by construction.
  const displayEntries: BuildingEntry[] | undefined = useMemo(
    () => (shouldFetchBuildings ? entries : []),
    [shouldFetchBuildings, entries],
  );
  const sortedEntries = useMemo(
    () => (displayEntries ? [...displayEntries].sort((a, b) => a.label.localeCompare(b.label)) : undefined),
    [displayEntries],
  );

  const previewTitle = (site?.name || draft.name || "Untitled site").trim();
  const previewLocation = useMemo(() => formatSiteAddress(draft) || "—", [draft]);
  const previewCapacity = draft.powerCapacityMw > 0 ? `${draft.powerCapacityMw} MW` : "—";
  const buildingCount = sortedEntries?.length ?? 0;
  const currentBuildingIds = useMemo(() => (displayEntries ?? []).map((e) => e.buildingId), [displayEntries]);

  // Picker confirm — apply the delta against the working set. `added` joins
  // entries without disturbing existing rows; `removed` drops only those
  // entries. Buildings in neither list are untouched, so a seeded building
  // the picker's listBuildings response omitted (race / paging gap) is
  // preserved. Mirrors ManageBuildingModal.handleManageRacksConfirm.
  const handleManageBuildingsConfirm = (delta: {
    added: { buildingId: bigint; label: string }[];
    removed: bigint[];
  }) => {
    const removedSet = new Set(delta.removed.map((id) => id.toString()));
    setEntries((prev) => {
      const kept = (prev ?? []).filter((e) => !removedSet.has(e.buildingId.toString()));
      const knownIds = new Set(kept.map((e) => e.buildingId.toString()));
      const newcomers: BuildingEntry[] = [];
      for (const a of delta.added) {
        if (knownIds.has(a.buildingId.toString())) continue;
        newcomers.push({ buildingId: a.buildingId, label: a.label, rackCount: 0n });
      }
      return [...kept, ...newcomers];
    });
    setShowManageBuildings(false);
  };

  // Kebab "Remove building" — drop it from the working set. Persisted on
  // Save as a `removed` delta entry, which moves the building to
  // "Unassigned" (the building itself is not deleted).
  const handleRemoveBuilding = useCallback((buildingId: bigint) => {
    setEntries((prev) => (prev ?? []).filter((e) => e.buildingId !== buildingId));
  }, []);

  const handleSave = async () => {
    const initial = initialIdsRef.current;
    const current = new Set((displayEntries ?? []).map((e) => e.buildingId.toString()));
    const added = [...current].filter((id) => !initial.has(id)).map((id) => BigInt(id));
    const removed = [...initial].filter((id) => !current.has(id)).map((id) => BigInt(id));
    const result = await onSave({ added, removed });
    if (!result) return;
    if (result.closeOnSuccess) onDismiss();
  };

  const buildingsBusy = saving || sortedEntries === undefined;

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title="Manage Site"
        onDismiss={onDismiss}
        isBusy={saving}
        buttons={[
          {
            text: "Delete site",
            variant: variants.secondaryDanger,
            onClick: onDeleteRequested,
            disabled: saving,
            testId: "manage-site-modal-delete",
          },
          {
            text: "Site settings",
            variant: variants.secondary,
            onClick: onEditDetails,
            disabled: saving,
            testId: "manage-site-modal-edit-details",
          },
          {
            text: "Manage buildings",
            variant: variants.secondary,
            onClick: () => setShowManageBuildings(true),
            // Create mode has no persisted site to assign buildings to yet.
            disabled: saving || mode === "create" || sortedEntries === undefined,
            testId: "manage-site-modal-manage-buildings",
          },
          {
            text: saving ? "Saving…" : "Save",
            variant: variants.primary,
            onClick: handleSave,
            disabled: saving,
            testId: "manage-site-modal-save",
          },
        ]}
        primaryPane={
          <div className="flex flex-col gap-6 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
            <section className="flex flex-col gap-3" data-testid="manage-site-modal-buildings-section">
              <Header
                title={`${buildingCount} ${buildingCount === 1 ? "building" : "buildings"}`}
                titleSize="text-heading-100"
              />
              {mode === "create" ? (
                <div className="rounded-xl border border-dashed border-border-5 p-4 text-300 text-text-primary-50">
                  Save the site first to add buildings.
                </div>
              ) : sortedEntries === undefined ? (
                <div className="text-300 text-text-primary-50">Loading…</div>
              ) : sortedEntries.length === 0 ? (
                <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-50">
                  <span>No buildings added to this site</span>
                  <Button
                    variant={variants.primary}
                    size={sizes.compact}
                    text="Add buildings"
                    onClick={() => setShowManageBuildings(true)}
                    disabled={buildingsBusy}
                    testId="manage-site-modal-empty-state-add"
                  />
                </div>
              ) : (
                <div className="flex flex-col divide-y divide-border-5" data-testid="manage-site-modal-buildings-list">
                  {sortedEntries.map((b) => (
                    <BuildingRow
                      key={b.buildingId.toString()}
                      buildingId={b.buildingId}
                      label={b.label}
                      rackCount={b.rackCount}
                      saving={saving}
                      onRemove={handleRemoveBuilding}
                    />
                  ))}
                </div>
              )}
            </section>
          </div>
        }
        secondaryPane={
          <div className="flex h-full min-h-0 flex-col">
            {/* Negative ml escapes wrapper laptop:pl-6 → labels land 20px from pane edge. */}
            <div className="flex shrink-0 items-start justify-between gap-4 pt-5 pr-5 pl-5 laptop:-ml-6 laptop:pl-5">
              <span className="min-w-0 truncate text-300 text-text-primary-50">
                {[previewTitle, previewLocation].filter((s) => s && s !== "—").join(", ") || previewTitle}
              </span>
              <span className="shrink-0 truncate text-300 text-text-primary-50">
                {[previewCapacity, `${buildingCount} ${buildingCount === 1 ? "building" : "buildings"}`]
                  .filter((s) => s && s !== "—")
                  .join(", ")}
              </span>
            </div>

            {/* Center the FPO building tiles both axes inside the remaining
                space so the preview reads as a centered floor plan. Real
                BuildingCard component lands in #263. */}
            <div className="flex flex-1 items-center justify-center p-5">
              <div className="flex flex-wrap justify-center gap-3" data-testid="manage-site-modal-building-grid">
                {sortedEntries === undefined ? (
                  <PlaceholderBlock label="Loading buildings…" className="h-20 w-[120px]" />
                ) : sortedEntries.length === 0 ? (
                  <PlaceholderBlock
                    label={mode === "create" ? "No buildings yet" : "No buildings in this site"}
                    className="h-20 w-[120px]"
                  />
                ) : (
                  sortedEntries.map((b) => (
                    <PlaceholderBlock key={b.buildingId.toString()} label={b.label} className="h-20 w-[120px]" />
                  ))
                )}
              </div>
            </div>
          </div>
        }
      />

      {showManageBuildings && site ? (
        <ManageBuildingsModal
          open={showManageBuildings}
          siteId={site.id}
          initialSelectedBuildingIds={currentBuildingIds}
          onDismiss={() => setShowManageBuildings(false)}
          onConfirm={handleManageBuildingsConfirm}
        />
      ) : null}
    </>
  );
};

export default ManageSiteModal;
