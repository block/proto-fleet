import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { formatPowerUsedCapacity, KW_PER_MW } from "@/shared/utils/telemetryFormat";

interface BuildingsListTableProps {
  buildings: BuildingWithCounts[];
  sites: SiteWithCounts[];
}

// Buildings tab list table. Rows navigate to /buildings/:id; the site
// column links indirectly by name so the operator can see which site
// owns each building at a glance. Real metric columns (hashrate, power,
// temperature, issues, health) land in Phase 1b once the building
// telemetry rollup is wired.
const BuildingsListTable = ({ buildings, sites }: BuildingsListTableProps) => {
  const navigate = useNavigate();

  const siteNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of sites) {
      if (!s.site) continue;
      map.set(s.site.id.toString(), s.site.name);
    }
    return map;
  }, [sites]);

  const ordered = useMemo(
    () => [...buildings].sort((a, b) => (a.building?.name ?? "").localeCompare(b.building?.name ?? "")),
    [buildings],
  );

  return (
    <div className="flex flex-col" data-testid="fleet-buildings-list">
      <div className="grid h-11 grid-cols-4 items-center gap-2 border-b border-border-5 px-3 text-emphasis-300 text-text-primary-50">
        <span>Building</span>
        <span>Site</span>
        <span>Racks</span>
        <span>Power</span>
      </div>
      {ordered.map((entry) => {
        const id = (entry.building?.id ?? 0n).toString();
        const siteId = entry.building?.siteId;
        const siteName = siteId ? (siteNameById.get(siteId.toString()) ?? "—") : "—";
        // Power capacity comes from the proto in kW. The shared formatter
        // converts to MW so we don't render "50000 kW" — matches
        // BuildingMetricsRow. Used side is null until Phase 1b telemetry
        // wiring lands.
        const powerCapacityKw = entry.building?.powerKw ?? 0;
        const power = formatPowerUsedCapacity(null, powerCapacityKw / KW_PER_MW) ?? "—";
        return (
          <button
            key={id}
            type="button"
            onClick={() => navigate(`/buildings/${id}`)}
            data-testid={`fleet-buildings-list-row-${id}`}
            className="hover:bg-surface-base-hover grid min-h-14 cursor-pointer grid-cols-4 items-center gap-2 border-b border-border-5 px-3 py-2 text-left last:border-b-0"
          >
            <span className="truncate text-emphasis-300">{entry.building?.name ?? "(unnamed)"}</span>
            <span className="truncate text-300 text-text-primary-50">{siteName}</span>
            <span className="truncate text-300">{entry.rackCount.toString()}</span>
            <span className="truncate text-300">{power}</span>
          </button>
        );
      })}
    </div>
  );
};

export default BuildingsListTable;
