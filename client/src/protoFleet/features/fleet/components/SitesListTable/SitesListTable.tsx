import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { formatSiteAddress } from "@/protoFleet/features/sites/formatAddress";
import { formatPowerUsedCapacity } from "@/shared/utils/telemetryFormat";

interface SitesListTableProps {
  sites: SiteWithCounts[];
}

// Sites tab list table. Mirrors the visual shape of the legacy
// SitesAllTable (open layout, hairline row dividers, three-column grid)
// but rows navigate to /sites/:id rather than changing the SitePicker
// selection — the picker stays on All Sites / Unassigned while the
// operator drills into a single site via the detail page.
//
// Columns land per the redesign plan once metric data is wired: name +
// location, infrastructure counts, power. Phase 1a shows the three
// columns with placeholder metrics; Phase 1b joins the telemetry feed.
const SitesListTable = ({ sites }: SitesListTableProps) => {
  const navigate = useNavigate();

  const ordered = useMemo(
    () => [...sites].sort((a, b) => (a.site?.name ?? "").localeCompare(b.site?.name ?? "")),
    [sites],
  );

  return (
    <div className="flex flex-col" data-testid="fleet-sites-list">
      <div className="grid h-11 grid-cols-3 items-center gap-2 border-b border-border-5 px-3 text-emphasis-300 text-text-primary-50">
        <span>Site</span>
        <span>Infrastructure</span>
        <span>Power / Efficiency</span>
      </div>
      {ordered.map((entry) => {
        const id = (entry.site?.id ?? 0n).toString();
        const location = formatSiteAddress(entry.site ?? {}) || "—";
        // Site capacity ships in MW; the shared formatter keeps the unit
        // consistent with SiteMetricsRow and the building list. Used side
        // is null until Phase 1b telemetry lands.
        const powerCapacityMw = entry.site?.powerCapacityMw ?? 0;
        const power = formatPowerUsedCapacity(null, powerCapacityMw) ?? "—";
        return (
          <button
            key={id}
            type="button"
            onClick={() => navigate(`/sites/${id}`)}
            data-testid={`fleet-sites-list-row-${id}`}
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
              <span className="truncate text-300">{power}</span>
              <span className="truncate text-300 text-text-primary-50">—</span>
            </div>
          </button>
        );
      })}
    </div>
  );
};

export default SitesListTable;
