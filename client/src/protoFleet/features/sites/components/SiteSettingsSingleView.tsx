import { useCallback, useEffect, useState } from "react";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { formatSiteAddress } from "@/protoFleet/features/sites/formatAddress";
import { Alert, ChevronDown, Ellipsis } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";

interface SiteSettingsSingleViewProps {
  site: SiteWithCounts;
  // Set of known site IDs (from the parent's ListSites cache) so the back-to-
  // all action can use the existing useActiveSite hook without double-wiring
  // its validation.
  knownSiteIds: Set<string>;
  // Opens ManageSiteModal in edit mode. Wired by the page so the modal
  // stack lives at the page level instead of nested per-section.
  onManage?: () => void;
  // Opens BuildingSettingsModal in create mode under this site. Hosted by
  // the page so building modals share a single useBuildingModals instance
  // across the surfaces that can launch them.
  onAddBuilding?: () => void;
  // Opens BuildingSettingsModal in edit mode for a specific row.
  onEditBuilding?: (row: BuildingWithCounts) => void;
  // Refresh signal — bumped by the page whenever the building cache might
  // have shifted (post-create / post-delete) so this view re-fetches.
  buildingsRefreshKey?: number;
}

// Visual mirrors the blockcell.sqprod.co prototype's single-site view:
// open layout (no card wrappers), 40px between major sections, hairline-
// bordered detail rows. Power-contract and Notes rows depend on backend
// follow-ups (#266 + power-contract migration); each row is rendered only
// when its backing field is present so the table never shows a half-filled
// shell.
const SiteSettingsSingleView = ({
  site,
  knownSiteIds,
  onManage,
  onAddBuilding,
  onEditBuilding,
  buildingsRefreshKey = 0,
}: SiteSettingsSingleViewProps) => {
  const { setActiveSite } = useActiveSite({ knownSiteIds });
  const siteId = site.site?.id ?? 0n;
  const { listBuildingsBySite } = useBuildings();
  // Pair buildings with the siteId that produced them so a stale
  // response (or a not-yet-resolved fetch after a site switch) can't
  // render rows from the prior site under the new site's header.
  // onEditBuilding would otherwise open a building from the prior
  // site with the new site name as context.
  const [buildingsResponse, setBuildingsResponse] = useState<
    { siteId: bigint; rows: BuildingWithCounts[] } | undefined
  >(undefined);
  const [buildingsError, setBuildingsError] = useState<string | null>(null);

  // Track buildings + buildingsError separately so transient failures don't
  // collapse into the "No buildings in this site yet." empty-state and look
  // like the site is empty when the request actually failed.
  const fetchBuildings = useCallback(() => {
    if (siteId === 0n) return undefined;
    const controller = new AbortController();
    void listBuildingsBySite({
      siteId,
      signal: controller.signal,
      onSuccess: (rows) => {
        setBuildingsResponse({ siteId, rows });
        setBuildingsError(null);
      },
      onError: (msg) => {
        setBuildingsError(msg);
        setBuildingsResponse({ siteId, rows: [] });
      },
    });
    return () => controller.abort();
  }, [listBuildingsBySite, siteId]);

  useEffect(() => fetchBuildings(), [fetchBuildings, buildingsRefreshKey]);

  // Only show rows whose paired siteId matches the active site —
  // otherwise treat as loading.
  const buildings = buildingsResponse && buildingsResponse.siteId === siteId ? buildingsResponse.rows : undefined;
  const displayBuildings = siteId === 0n ? [] : buildings;

  const addressLine = formatSiteAddress(site.site ?? {}, { includeCountry: true });
  const powerCapacity = site.site?.powerCapacityMw ? `${site.site.powerCapacityMw} MW` : "—";
  const timezone = site.site?.timezone || "—";
  const notes = site.site?.notes ?? "";

  return (
    <div className="flex flex-col gap-10" data-testid="site-settings-single-view">
      <div>
        <div className="mb-6 flex items-center justify-between">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="All sites"
            // ChevronDown rotated 90° (clockwise) points left, standing in
            // for a ChevronLeft icon we don't ship separately.
            prefixIcon={<ChevronDown width="w-3" className="rotate-90" />}
            onClick={() => setActiveSite({ kind: "all" })}
            testId="site-settings-back-to-all"
          />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Manage site"
            onClick={onManage ?? (() => undefined)}
            disabled={!onManage}
            testId="site-settings-manage"
          />
        </div>
        <Header title={site.site?.name ?? "(unnamed)"} titleSize="text-heading-200" description={addressLine || "—"} />
      </div>

      <section className="flex flex-col">
        <div className="mb-3 flex items-center justify-between">
          <Header title="Details" titleSize="text-heading-100" />
        </div>
        <DetailRow label="Power" value={`— / ${powerCapacity}`} />
        <DetailRow label="Timezone" value={timezone} />
        {notes ? <DetailRow label="Notes" value={notes} /> : null}
        {/*
          PUE / Gateway / Power-contract rows depend on backend follow-ups
          (gateway likely arrives via the fleet-node workstream; power-
          contract columns are deferred). Each row is gated on its
          underlying field so we don't render empty shells.
        */}
      </section>

      <section className="flex flex-col">
        <div className="mb-3 flex items-center justify-between">
          <Header title="Buildings" titleSize="text-heading-100" />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Add building"
            onClick={onAddBuilding ?? (() => undefined)}
            disabled={!onAddBuilding}
            testId="site-settings-add-building"
          />
        </div>
        {buildingsError ? (
          <Callout
            intent="danger"
            prefixIcon={<Alert />}
            title="Couldn't load buildings"
            subtitle={buildingsError}
            buttonText="Retry"
            buttonOnClick={fetchBuildings}
            testId="site-settings-buildings-error"
          />
        ) : displayBuildings === undefined ? (
          <div className="text-300 text-text-primary-50">Loading…</div>
        ) : displayBuildings.length === 0 ? (
          <div className="text-300 text-text-primary-50">No buildings in this site yet.</div>
        ) : (
          <BuildingsTable buildings={displayBuildings} onEditBuilding={onEditBuilding} />
        )}
      </section>
    </div>
  );
};

interface DetailRowProps {
  label: string;
  value: string;
}

// Mirrors the prototype's .settings-card-row: flex justify-between, no
// horizontal padding, hairline divider on top, both label and value at
// text-primary. Borders compose so a stack of DetailRows reads as a list.
const DetailRow = ({ label, value }: DetailRowProps) => (
  <div className="flex items-center justify-between border-t border-border-5 py-3 text-300">
    <span className="text-text-primary">{label}</span>
    <span className="text-text-primary">{value}</span>
  </div>
);

interface BuildingsTableProps {
  buildings: BuildingWithCounts[];
  onEditBuilding?: (row: BuildingWithCounts) => void;
}

// Mirrors the prototype's .site-cfg-table with the cols-4 grid template
// (1.2fr 1fr 0.6fr 32px). Type column hides until #267 lands building_type;
// until then the cell renders an em-dash so the grid stays balanced.
const BuildingsTable = ({ buildings, onEditBuilding }: BuildingsTableProps) => (
  <div className="flex flex-col">
    <div
      className="grid h-11 items-center gap-2 border-b border-border-5 text-emphasis-300 text-text-primary-50"
      style={{ gridTemplateColumns: "1.2fr 1fr 0.6fr 32px" }}
    >
      <span>Name</span>
      <span>Type</span>
      <span>Power</span>
      <span />
    </div>
    {buildings.map((b) => {
      const id = (b.building?.id ?? 0n).toString();
      const name = b.building?.name ?? "(unnamed)";
      const powerMw = b.building?.powerKw ? `${(b.building.powerKw / 1000).toFixed(1)} MW` : "—";
      const clickable = !!onEditBuilding;
      return (
        <div
          key={id}
          role={clickable ? "button" : undefined}
          tabIndex={clickable ? 0 : undefined}
          className={`grid h-12 items-center gap-2 border-b border-border-5 ${
            clickable ? "hover:bg-surface-base-hover cursor-pointer" : ""
          }`}
          style={{ gridTemplateColumns: "1.2fr 1fr 0.6fr 32px" }}
          onClick={clickable ? () => onEditBuilding?.(b) : undefined}
          onKeyDown={
            clickable
              ? (e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onEditBuilding?.(b);
                  }
                }
              : undefined
          }
          data-testid={`site-settings-building-row-${id}`}
        >
          <span className="truncate text-emphasis-300">{name}</span>
          {/* building_type follow-up: #267. Em-dash until backend ships. */}
          <span className="truncate text-300 text-text-primary-50">—</span>
          <span className="truncate text-300">— / {powerMw}</span>
          <button
            type="button"
            aria-label={`Actions for ${name}`}
            // Per-row overflow menu lands alongside richer building actions; for
            // PR 3 the row-click handles edit so the kebab stays disabled.
            onClick={(e) => e.stopPropagation()}
            disabled
            className="hover:bg-surface-base-hover flex h-7 w-7 items-center justify-center rounded-lg text-text-primary-50 disabled:opacity-40"
            data-testid={`site-settings-building-actions-${id}`}
          >
            <Ellipsis />
          </button>
        </div>
      );
    })}
  </div>
);

export default SiteSettingsSingleView;
