import { useMemo } from "react";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";

interface SitesAllTableProps {
  sites: SiteWithCounts[];
}

// Flat all-sites table for /settings/sites All Sites mode. Rows ordered by
// site.name ascending; not user-sortable in Phase 1. Clicking a row narrows
// the SitePicker selection to that site (which in turn re-renders the page
// in single-site mode). Power / efficiency / miner counts render as "—"
// until #263 wires real metrics through.
//
// Visual mirrors the blockcell.sqprod.co prototype: open list with hairline
// row dividers, no outer card container, three-column CSS grid. Column
// widths use repeat(3, 1fr) to match the prototype's even split.
const SitesAllTable = ({ sites }: SitesAllTableProps) => {
  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { setActiveSite } = useActiveSite({ knownSiteIds });

  const ordered = useMemo(
    () => [...sites].sort((a, b) => (a.site?.name ?? "").localeCompare(b.site?.name ?? "")),
    [sites],
  );

  return (
    <div className="flex flex-col" data-testid="sites-all-table">
      <div className="grid h-11 grid-cols-3 items-center gap-2 border-b border-border-5 px-3 text-emphasis-300 text-text-primary-50">
        <span>Site</span>
        <span>Infrastructure</span>
        <span>Power / Efficiency</span>
      </div>
      {ordered.map((entry) => {
        const id = (entry.site?.id ?? 0n).toString();
        const cityState = [entry.site?.locationCity, entry.site?.locationState].filter(Boolean).join(", ");
        const locationParts = [entry.site?.address, cityState, entry.site?.postalCode].filter(Boolean);
        const location = locationParts.length > 0 ? locationParts.join(" • ") : "—";
        const powerCapacity = entry.site?.powerCapacityMw ? `${entry.site.powerCapacityMw} MW` : "—";
        return (
          <button
            key={id}
            type="button"
            onClick={() => setActiveSite({ kind: "site", id })}
            data-testid={`sites-all-table-row-${id}`}
            className="hover:bg-surface-base-hover grid min-h-14 cursor-pointer grid-cols-3 items-center gap-2 border-b border-border-5 px-3 py-2 text-left last:border-b-0"
          >
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="truncate text-emphasis-300">{entry.site?.name ?? "(unnamed)"}</span>
              <span className="truncate text-300 text-text-primary-50">{location}</span>
            </div>
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="truncate text-300">{entry.buildingCount.toString()} buildings</span>
              <span className="truncate text-300 text-text-primary-50">{entry.deviceCount.toString()} miners</span>
            </div>
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="truncate text-300">— / {powerCapacity}</span>
              <span className="truncate text-300 text-text-primary-50">—</span>
            </div>
          </button>
        );
      })}
    </div>
  );
};

export default SitesAllTable;
