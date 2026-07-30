import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import ManageBuildingsModal from "../ManageBuildingsModal";
import { type BuildingFormValues, emptyBuildingFormValues, useBuildings } from "@/protoFleet/api/buildings";
import { type Building, type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type Site, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type SiteFormValues } from "@/protoFleet/api/sites";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import BuildingSettingsModal from "@/protoFleet/features/buildings/components/BuildingSettingsModal";
import { formatSiteAddress } from "@/protoFleet/features/sites/formatAddress";
import { Ellipsis } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

// One building shown in the modal's list. Seeded from the server fetch and
// re-synced after each membership write (the picker's Save and the row-level
// Remove both commit immediately, so this list mirrors the server rather than
// staging a pending edit).
interface BuildingEntry {
  buildingId: bigint;
  label: string;
  rackCount: bigint;
}

interface ManageSiteModalProps {
  open: boolean;
  draft: SiteFormValues;
  // The site is always persisted by the time this modal opens (the create
  // flow's Continue creates it up front), so it drives the right-pane preview
  // header and the site_id for building writes.
  site: Site;
  // Applies the buildings picker's membership delta via AssignBuildingsToSite.
  // Resolves true on success; a false result leaves the picker open to retry.
  onAssignBuildings: (delta: { added: bigint[]; removed: bigint[] }) => Promise<boolean>;
  // Row-level "Remove building" — unassigns it from this site immediately.
  onRemoveBuilding: (buildingId: bigint, label: string) => Promise<boolean>;
  // Creates a new building against this site and returns the created row (or
  // null on failure). The building is associated to the site atomically, so
  // the modal injects the returned row into its working set without a refetch.
  onCreateBuilding: (values: BuildingFormValues) => Promise<Building | null>;
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
  // Counts of items assigned directly to this site (not via a building),
  // shown as count lines under the buildings list. Set when the site was
  // created from a bulk "New site" action seeded with loose racks/miners.
  unassignedRackCount?: number;
  unassignedMinerCount?: number;
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
  onRemove: (buildingId: bigint, label: string) => void;
}) => {
  const [showMenu, setShowMenu] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const handleRemove = useCallback(() => {
    setShowMenu(false);
    onRemove(buildingId, label);
  }, [buildingId, label, onRemove]);

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
  draft,
  site,
  onAssignBuildings,
  onRemoveBuilding,
  onCreateBuilding,
  onEditDetails,
  onDeleteRequested,
  onDismiss,
  saving = false,
  buildingsRefreshKey = 0,
  unassignedRackCount,
  unassignedMinerCount,
}: ManageSiteModalProps) => {
  const { listBuildingsBySite } = useBuildings();
  // undefined = loading; [] = loaded-empty. Mirrors the site's committed
  // membership — every mutation in this modal writes before updating it.
  const [entries, setEntries] = useState<BuildingEntry[] | undefined>(undefined);
  const [showManageBuildings, setShowManageBuildings] = useState(false);
  const [showCreateBuilding, setShowCreateBuilding] = useState(false);

  const shouldFetchBuildings = open;
  const fetchSiteId = site.id;
  useEffect(() => {
    if (!shouldFetchBuildings) return;
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
      },
      onError: () => {
        setEntries([]);
      },
    });
    return () => controller.abort();
  }, [shouldFetchBuildings, fetchSiteId, listBuildingsBySite, buildingsRefreshKey]);

  const sortedEntries = useMemo(
    () => (entries ? [...entries].sort((a, b) => a.label.localeCompare(b.label)) : undefined),
    [entries],
  );

  const previewTitle = (site.name || draft.name || "Untitled site").trim();
  const previewLocation = useMemo(() => formatSiteAddress(draft) || "—", [draft]);
  const previewCapacity = draft.powerCapacityMw > 0 ? `${draft.powerCapacityMw} MW` : "—";
  const buildingCount = sortedEntries?.length ?? 0;
  const currentBuildingIds = useMemo(() => (entries ?? []).map((e) => e.buildingId), [entries]);
  // The inline building-create dropdown is always locked to this site, so a
  // one-element list built from `site` is all BuildingSettingsModal needs — and
  // it sidesteps the brief window right after create-on-Continue where the
  // page's site catalog hasn't refetched the new row yet.
  const buildingCreateSites = useMemo(() => [create(SiteWithCountsSchema, { site })], [site]);

  // Picker Save — the write already happened by the time this resolves, so
  // mirror it into the list. `added` joins without disturbing existing rows;
  // `removed` drops only those entries. Buildings in neither list are
  // untouched, so a member the picker's listBuildings response omitted (race /
  // paging gap) is preserved. A failed write leaves the picker open to retry.
  const handleManageBuildingsConfirm = async (delta: {
    added: { buildingId: bigint; label: string }[];
    removed: bigint[];
  }) => {
    const ok = await onAssignBuildings({ added: delta.added.map((a) => a.buildingId), removed: delta.removed });
    if (!ok) return;
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

  // Kebab "Remove building" — unassigns immediately (moves the building to
  // "Unassigned"; the building itself is not deleted). Drop the row only once
  // the write lands so a failure leaves the list truthful.
  const handleRemoveBuilding = useCallback(
    async (buildingId: bigint, label: string) => {
      const ok = await onRemoveBuilding(buildingId, label);
      if (!ok) return;
      setEntries((prev) => (prev ?? []).filter((e) => e.buildingId !== buildingId));
    },
    [onRemoveBuilding],
  );

  // Inline building-create confirm. CreateBuilding already associated the new
  // building to this site, so inject it into the list rather than refetching.
  // A failed create returns null (toast shown by the host) and leaves the
  // create modal open.
  const handleCreateBuildingSave = async (values: BuildingFormValues) => {
    const created = await onCreateBuilding(values);
    if (!created) return;
    setEntries((prev) => {
      const next = prev ?? [];
      if (next.some((e) => e.buildingId === created.id)) return next;
      return [...next, { buildingId: created.id, label: created.name, rackCount: 0n }];
    });
    setShowCreateBuilding(false);
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
            // Only blocked while the building list is still loading
            // (sortedEntries undefined).
            disabled: saving || sortedEntries === undefined,
            testId: "manage-site-modal-manage-buildings",
          },
          {
            // Placeholder. Building membership now commits in the picker (and
            // row-level Remove commits on click), so this modal owns only
            // building placement within the site — which has no backend yet and
            // is not tracked by an issue. Until then Save writes nothing and
            // just closes; deliberately no success toast, since there's nothing
            // to report.
            text: "Save",
            variant: variants.primary,
            onClick: onDismiss,
            disabled: buildingsBusy,
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
              {sortedEntries === undefined ? (
                <div className="text-300 text-text-primary-50">Loading…</div>
              ) : sortedEntries.length === 0 ? (
                <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-50">
                  <span>No buildings added to this site</span>
                  {/* Single affordance — the picker itself carries the
                      "New building" hand-off for creating one from scratch. */}
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
              {unassignedRackCount ? (
                <p className="text-200 text-text-primary-50" data-testid="manage-site-unassigned-racks">
                  {unassignedRackCount} {unassignedRackCount === 1 ? "rack" : "racks"} unassigned to a building
                </p>
              ) : null}
              {unassignedMinerCount ? (
                <p className="text-200 text-text-primary-50" data-testid="manage-site-unassigned-miners">
                  {unassignedMinerCount} {unassignedMinerCount === 1 ? "miner" : "miners"} unassigned to a building
                </p>
              ) : null}
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
                  <PlaceholderBlock label="No buildings in this site" className="h-20 w-[120px]" />
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

      {showManageBuildings ? (
        <ManageBuildingsModal
          open={showManageBuildings}
          siteId={site.id}
          initialSelectedBuildingIds={currentBuildingIds}
          onDismiss={() => setShowManageBuildings(false)}
          onConfirm={handleManageBuildingsConfirm}
          saving={saving}
          // "New building" swaps the picker for the create modal, abandoning
          // any unsaved checkbox changes — the picker's Save is what commits
          // membership, so leaving without it writes nothing.
          onCreateNewLaunch={() => {
            setShowManageBuildings(false);
            setShowCreateBuilding(true);
          }}
        />
      ) : null}

      {showCreateBuilding ? (
        // Create a building already attached to this site. The Site dropdown is
        // locked to `site` (initialSiteId set), matching the site-scoped
        // building-create entry point elsewhere.
        <BuildingSettingsModal
          open
          mode="create"
          initialValues={emptyBuildingFormValues()}
          sites={buildingCreateSites}
          initialSiteId={site.id}
          parentSiteLabel={previewTitle}
          onSave={async (values) => {
            await handleCreateBuildingSave(values);
          }}
          onDismiss={() => setShowCreateBuilding(false)}
          saving={saving}
        />
      ) : null}
    </>
  );
};

export default ManageSiteModal;
