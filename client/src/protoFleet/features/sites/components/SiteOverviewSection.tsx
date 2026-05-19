import { useEffect, useState } from "react";

import PlaceholderBlock from "./PlaceholderBlock";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import BuildingCard from "@/protoFleet/features/buildings/components/BuildingCard";
import Header from "@/shared/components/Header";

interface SiteOverviewSectionProps {
  site: SiteWithCounts;
}

const SiteOverviewSection = ({ site }: SiteOverviewSectionProps) => {
  const siteId = site.site?.id ?? 0n;
  const { listBuildingsBySite } = useBuildings();
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);

  useEffect(() => {
    // Defensive: siteId is 0n only when the upstream SiteWithCounts is
    // missing its inner site message, which shouldn't happen in practice.
    // If it does, the server rejects the request and onError sets [].
    if (siteId === 0n) return;
    void listBuildingsBySite({
      siteId,
      onSuccess: setBuildings,
      onError: () => setBuildings([]),
    });
  }, [listBuildingsBySite, siteId]);

  const displayBuildings = siteId === 0n ? [] : buildings;

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
      {displayBuildings === undefined ? (
        <PlaceholderBlock label="Loading buildings…" className="h-32" />
      ) : displayBuildings.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70">
          No buildings in this site yet.
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 laptop:grid-cols-3 phone:grid-cols-2">
          {displayBuildings.map((b) => (
            <BuildingCard key={(b.building?.id ?? 0n).toString()} building={b} siteId={siteId} />
          ))}
        </div>
      )}
    </section>
  );
};

export default SiteOverviewSection;
