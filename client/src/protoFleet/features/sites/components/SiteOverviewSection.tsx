import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import BuildingCard from "@/protoFleet/features/buildings/components/BuildingCard";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";

interface SiteOverviewSectionProps {
  site: SiteWithCounts;
  // Buildings for this site, supplied by the parent SitesPage after a single
  // ListBuildings call. `undefined` indicates "still loading"; `[]` indicates
  // "no buildings in this site".
  buildings: BuildingWithCounts[] | undefined;
}

const SiteOverviewSection = ({ site, buildings }: SiteOverviewSectionProps) => {
  const siteId = site.site?.id ?? 0n;

  return (
    <section
      className="flex flex-col gap-6 rounded-xl border border-border-5 p-6"
      data-testid={`site-overview-section-${siteId.toString()}`}
    >
      <Header title={site.site?.name ?? "(unnamed)"} titleSize="text-heading-300" />
      <PlaceholderBlock
        label="Metrics row (Location, Hashrate, Power, Efficiency, Buildings) — #263"
        className="h-20"
      />
      {buildings === undefined ? (
        <PlaceholderBlock label="Loading buildings…" className="h-32" />
      ) : buildings.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70">
          No buildings in this site yet.
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 laptop:grid-cols-3 phone:grid-cols-2">
          {buildings.map((b) => (
            <BuildingCard key={(b.building?.id ?? 0n).toString()} building={b} />
          ))}
        </div>
      )}
    </section>
  );
};

export default SiteOverviewSection;
