import SiteMetricsRow from "./SiteMetricsRow";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSiteStats } from "@/protoFleet/api/useSiteStats";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import BuildingCard from "@/protoFleet/features/buildings/components/BuildingCard";

interface SiteOverviewSectionProps {
  site: SiteWithCounts;
  // Buildings for this site, supplied by the parent SitesPage after a single
  // ListBuildings call. `undefined` indicates "still loading"; `[]` indicates
  // "no buildings in this site".
  buildings: BuildingWithCounts[] | undefined;
}

// Per-site overview section — flat metric row at the top, then a responsive
// grid of BuildingCards. Matches the prototype: no card wrapper around the
// site itself; the metric row identifies the site via its Location tile.
//
// Telemetry roll-up comes from SiteService.GetSiteStats — server-side join
// covers both racked devices and site-direct (un-racked) devices, so the
// metric row matches the miner list exactly.
const SiteOverviewSection = ({ site, buildings }: SiteOverviewSectionProps) => {
  const siteId = site.site?.id ?? 0n;
  const siteIdText = siteId.toString();
  const hasSiteId = siteId !== 0n;

  // Poll so the metric row stays live as miners flip state on the
  // dashboard without forcing the operator to refresh the page.
  const {
    stats,
    error: statsError,
    refetch: refetchStats,
  } = useSiteStats({ siteId, enabled: hasSiteId, pollIntervalMs: POLL_INTERVAL_MS });

  // Site name renders above the metric row so two sites with blank or
  // duplicate locations are still distinguishable in the All-Sites view.
  // Location remains a tile in the metric row for at-a-glance scanning;
  // the name is the authoritative identifier.
  const siteName = site.site?.name ?? "(unnamed site)";

  return (
    <section className="flex flex-col gap-6" data-testid={`site-overview-section-${siteIdText}`}>
      <h2 className="text-emphasis-300 text-text-primary" data-testid={`site-overview-section-${siteIdText}-name`}>
        {siteName}
      </h2>
      {statsError ? (
        <div
          className="flex items-center justify-between gap-3 rounded-xl border border-intent-critical-20 bg-intent-critical-10 px-4 py-3 text-200 text-intent-critical-text"
          data-testid={`site-overview-section-${siteIdText}-error`}
        >
          <span>Couldn&apos;t load site metrics: {statsError}</span>
          <button
            type="button"
            onClick={() => refetchStats()}
            className="shrink-0 underline hover:opacity-80"
            data-testid={`site-overview-section-${siteIdText}-retry`}
          >
            Retry
          </button>
        </div>
      ) : null}
      <SiteMetricsRow
        locationCity={site.site?.locationCity ?? ""}
        locationState={site.site?.locationState ?? ""}
        powerCapacityMw={site.site?.powerCapacityMw ?? 0}
        // Prefer the fresh `buildingCount` from GetSiteStats once it
        // lands so the tile reflects buildings added/removed while the
        // page stays open. Fall back to the initial ListSites count
        // during the first paint.
        buildingCount={stats?.buildingCount ?? Number(site.buildingCount)}
        metrics={stats}
      />
      {buildings === undefined ? (
        <div className="text-200 text-text-primary-50">Loading buildings…</div>
      ) : buildings.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70">
          No buildings in this site yet.
        </div>
      ) : (
        <div className="grid auto-rows-fr grid-cols-1 gap-4 tablet:grid-cols-2 laptop:grid-cols-3">
          {buildings.map((b) => (
            <BuildingCard key={(b.building?.id ?? 0n).toString()} building={b} />
          ))}
        </div>
      )}
    </section>
  );
};

export default SiteOverviewSection;
