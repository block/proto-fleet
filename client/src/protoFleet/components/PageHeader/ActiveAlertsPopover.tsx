import { Link } from "react-router-dom";
import clsx from "clsx";

import { countLabel } from "@/protoFleet/features/alerts/lib/alertCountLabels";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

interface ActiveAlertsPopoverProps {
  groups: ActiveAlertGroup[];
  error: string | null;
  hasMore: boolean;
  onSelectGroup: (group: ActiveAlertGroup) => void;
  onNavigateToAlerts: () => void;
}

// The rollup names the rule rather than any one dimension a device-less rule fired on, so the count reports the rest.
const groupTitle = (group: ActiveAlertGroup) =>
  group.device_count === 0 && group.alert_count > 1
    ? `${group.alert_name} — ${countLabel(group.alert_count, "instance")}`
    : group.alert_name;

// Affected miners are the blast radius worth leading with; a device-less rule has none to report.
const groupDetail = (group: ActiveAlertGroup) =>
  group.device_count > 0 ? `${countLabel(group.device_count, "miner")} affected` : "";

const ActiveAlertsPopover = ({
  groups,
  error,
  hasMore,
  onSelectGroup,
  onNavigateToAlerts,
}: ActiveAlertsPopoverProps) => {
  return (
    <div className="flex flex-col">
      {error ? <div className="pb-3 text-200 leading-snug text-intent-critical-fill">{error}</div> : null}

      <div className="flex max-h-80 flex-col overflow-y-auto">
        {groups.map((group, groupIndex) => {
          const detail = groupDetail(group);
          // Empty when the server sent no start time; the bare "Firing since" label would read as broken.
          const firingSince = formatTimestamp(isoToEpochSeconds(group.first_started_at));

          // The rollup says nothing about what any one instance reported, so every row opens the drill-in.
          return (
            <button
              key={group.key}
              type="button"
              onClick={() => onSelectGroup(group)}
              className={clsx(
                "flex flex-col gap-1 rounded-xl py-2 text-left transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5",
                groupIndex > 0 && "border-t border-border-5",
              )}
            >
              <div className="flex items-start gap-2">
                <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-intent-warning-fill" />
                <span className="min-w-0 text-heading-100 text-text-primary">{groupTitle(group)}</span>
              </div>
              <div className="pl-4">
                {detail ? <div className="text-200 leading-snug text-text-primary-70">{detail}</div> : null}
                {firingSince ? (
                  <div className="text-200 leading-snug text-text-primary-50">Firing since {firingSince}</div>
                ) : null}
              </div>
            </button>
          );
        })}
      </div>

      {hasMore ? (
        <div className="pt-2 text-200 leading-snug text-text-primary-50">
          Showing the first {groups.length} alerts; additional firing alerts are not shown.
        </div>
      ) : null}

      <div className="mt-3 border-t border-border-5 pt-3">
        <Link
          to="/settings/alerts"
          onClick={onNavigateToAlerts}
          className="block rounded-xl px-3 py-2.5 text-emphasis-300 text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
        >
          View all alerts
        </Link>
      </div>
    </div>
  );
};

export default ActiveAlertsPopover;
