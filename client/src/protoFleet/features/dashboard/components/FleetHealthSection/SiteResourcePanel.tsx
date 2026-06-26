import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { useCardCarousel } from "./useCardCarousel";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import BuildingSummaryCard from "@/protoFleet/features/buildings/components/BuildingSummaryCard";
import { RackCard } from "@/protoFleet/features/fleetManagement/components/RackCard";
import { encodeFilterToURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { mapRackToCardProps } from "@/protoFleet/features/fleetManagement/utils/rackCardMapper";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import { type DeviceSetSiteFilter, useDeviceSetListState } from "@/protoFleet/hooks/useDeviceSetListState";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useTemperatureUnit } from "@/protoFleet/store";
import { type ActiveSite } from "@/protoFleet/store/types/activeSite";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import DurationSelector from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

// Tab labels double as the selector values (the DurationSelector renders the
// value as the button text).
type TabKey = "Buildings" | "Racks" | "Components";
const TABS: readonly TabKey[] = ["Buildings", "Racks", "Components"];

// Statuses that count as "needs attention" — the Components "View all" links
// to the miner list filtered to these, mirroring the SiteCard badge.
const NEEDS_ATTENTION_STATUSES = [
  DeviceStatus.ERROR,
  DeviceStatus.NEEDS_MINING_POOL,
  DeviceStatus.UPDATING,
  DeviceStatus.REBOOT_REQUIRED,
];

// One page of racks is plenty for the gallery; the operator drills into
// /fleet/racks for the full, paginated view.
const RACKS_PAGE_SIZE = 24;

// Filter that resolves to "match nothing" so useDeviceSetListState skips the
// network call entirely while the Racks tab is inactive.
const MATCH_NONE: DeviceSetSiteFilter = { siteIds: [], includeUnassigned: false, matchNone: true };

interface SiteResourcePanelProps {
  siteId: bigint;
  activeSite: ActiveSite;
}

// The bottom half of the Fleet health module: a button selector switching
// between the site's Buildings, Racks, and Components. Buildings/Racks render
// as a horizontal card gallery that overflows the card and slides via the
// chevrons in the toolbar; Components renders the FleetErrors breakdown.
const SiteResourcePanel = ({ siteId, activeSite }: SiteResourcePanelProps) => {
  const [tab, setTab] = useState<TabKey>("Buildings");
  const navigate = useNavigate();
  const temperatureUnit = useTemperatureUnit();

  // Buildings — fetched only while the Buildings tab is active.
  const { listBuildingsBySite } = useBuildings();
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);
  useEffect(() => {
    if (tab !== "Buildings" || siteId === 0n) return;
    const controller = new AbortController();
    void listBuildingsBySite({
      siteId,
      signal: controller.signal,
      onSuccess: setBuildings,
      onError: () => setBuildings([]),
    });
    return () => controller.abort();
  }, [tab, siteId, listBuildingsBySite]);

  // Racks — gated via the site filter: an inactive tab resolves to matchNone,
  // which short-circuits the fetch inside useDeviceSetListState.
  const { listRacks } = useDeviceSets();
  const getSiteFilter = useCallback<() => DeviceSetSiteFilter>(
    () => (tab === "Racks" ? { siteIds: [siteId], includeUnassigned: false } : MATCH_NONE),
    [tab, siteId],
  );
  const {
    deviceSets: racks,
    statsMap,
    hasCompletedInitialFetch: racksLoaded,
  } = useDeviceSetListState(listRacks, RACKS_PAGE_SIZE, undefined, undefined, undefined, getSiteFilter);

  // Component errors — fetched only while the Components tab is active.
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors({
    siteIds: [siteId],
    includeUnassigned: false,
    enabled: tab === "Components",
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  // Re-key the carousel whenever the visible row changes so it re-measures and
  // resets to the start.
  const galleryKey =
    tab === "Buildings" ? `buildings:${buildings?.length ?? "loading"}` : `racks:${racks.length}:${racksLoaded}`;
  const carousel = useCardCarousel(galleryKey);

  // "View all" target per tab — the fleet page for buildings/racks, and the
  // needs-attention-filtered miner list for components. All scoped to the site.
  const viewAllHref = useMemo(() => {
    if (tab === "Buildings") return scopedPath("/fleet/buildings", activeSite);
    if (tab === "Racks") return scopedPath("/fleet/racks", activeSite);
    const params = encodeFilterToURL(create(MinerListFilterSchema, { deviceStatus: NEEDS_ATTENTION_STATUSES }));
    return scopedPath(`/fleet/miners?${params.toString()}`, activeSite);
  }, [tab, activeSite]);

  const renderGallery = () => {
    if (tab === "Buildings") {
      if (buildings === undefined) return <GallerySkeleton itemClassName="h-32 w-48" />;
      if (buildings.length === 0) return <GalleryEmpty label="No buildings in this site yet." />;
      return buildings.map((building) => (
        <div key={(building.building?.id ?? 0n).toString()} className="w-48 shrink-0">
          <BuildingSummaryCard building={building} />
        </div>
      ));
    }
    // Racks
    if (!racksLoaded) return <GallerySkeleton itemClassName="h-44 w-[300px]" />;
    if (racks.length === 0) return <GalleryEmpty label="No racks in this site yet." />;
    return racks.map((rack) => (
      <div key={rack.id.toString()} className="w-[300px] shrink-0">
        <RackCard
          label={rack.label}
          {...mapRackToCardProps(rack, statsMap.get(rack.id), temperatureUnit)}
          showMetrics={false}
          onClick={() => navigate(`/racks/${rack.id}`)}
        />
      </div>
    ));
  };

  return (
    <div className="mt-10">
      {/* Full-width divider between the health summary and this panel. */}
      <div className="-mx-10 mb-6 h-px bg-border-5 phone:-mx-6" />

      <div className="flex items-center justify-between gap-4">
        <DurationSelector duration={tab} durations={TABS} onSelect={setTab} />
        <div className="flex items-center gap-2">
          {tab !== "Components" && carousel.hasOverflow ? (
            <div className="flex items-center gap-1">
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                ariaLabel="Previous"
                disabled={!carousel.canPrev}
                onClick={carousel.prev}
                prefixIcon={<ChevronDown className="rotate-90" />}
                testId="site-resource-prev"
              />
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                ariaLabel="Next"
                disabled={!carousel.canNext}
                onClick={carousel.next}
                prefixIcon={<ChevronDown className="-rotate-90" />}
                testId="site-resource-next"
              />
            </div>
          ) : null}
          <Link to={viewAllHref} data-testid="site-resource-view-all">
            <Button variant={variants.secondary} size={sizes.compact} text="View all" />
          </Link>
        </div>
      </div>

      <div className="mt-6">
        {tab === "Components" ? (
          <FleetErrors
            controlBoardErrors={controlBoardErrors}
            fanErrors={fanErrors}
            hashboardErrors={hashboardErrors}
            psuErrors={psuErrors}
            activeSite={activeSite}
          />
        ) : (
          // Outer clip breaks out to the card's edges so cards stay visible
          // through the padding as they slide (cut off only at the card edge).
          // The measured viewport stays at content width (mx = card padding),
          // so the slide clamps with the last card flush to the content edge —
          // not the card edge — leaving the gutter clear at the end.
          <div className="-mx-10 overflow-hidden phone:-mx-6" data-testid="site-resource-gallery">
            <div ref={carousel.viewportRef} className="mx-10 phone:mx-6">
              <div
                ref={carousel.trackRef}
                className="flex items-stretch gap-1 transition-transform duration-300 ease-out"
                style={{ transform: `translateX(-${carousel.translatePx}px)` }}
              >
                {renderGallery()}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

const GallerySkeleton = ({ itemClassName }: { itemClassName: string }) => (
  <>
    {Array.from({ length: 4 }).map((_, i) => (
      <SkeletonBar key={i} className={`shrink-0 rounded-xl ${itemClassName}`} />
    ))}
  </>
);

const GalleryEmpty = ({ label }: { label: string }) => (
  <div className="w-full rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70">
    {label}
  </div>
);

export default SiteResourcePanel;
