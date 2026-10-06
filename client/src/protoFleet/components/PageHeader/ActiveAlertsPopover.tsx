import { Link } from "react-router-dom";
import clsx from "clsx";

import { activeGroupTitle } from "@/protoFleet/features/alerts/lib/activeGroupTitle";
import { countLabel } from "@/protoFleet/features/alerts/lib/alertCountLabels";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

interface ActiveAlertsPopoverProps {
  groups: ActiveAlertGroup[];
  error: string | null;
  hasMore: boolean;
  onSelectGroup: (group: ActiveAlertGroup) => void;
  onNavigateToAlerts: () => void;
}

// The blast radius, in whichever unit the rule fires on. A single device-less instance is the one row with
// nothing to count, so it reports the summary the server sent for it instead.
const groupDetail = (group: ActiveAlertGroup) => {
  if (group.device_count > 0) {
    return `${countLabel(group.device_count, "miner")} affected`;
  }
  return group.alert_count > 1 ? countLabel(group.alert_count, "instance") : group.summary;
};

// A drill-in earns its place when the row can't say the whole thing: several miners, several device-less
// instances, or a lone instance whose summary the server withheld — the rule's own templates decide that, so a
// row cannot assume it has one. Only a single instance that did arrive with its summary is complete as it is.
const isDrillable = (group: ActiveAlertGroup) =>
  group.device_count > 0 || group.alert_count > 1 || group.summary === "";

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

      {/* Focusable so a list of non-drillable rows, which hold no tab stop of their own, can still be scrolled. */}
      <div className="flex max-h-80 flex-col overflow-y-auto" tabIndex={0}>
        {groups.map((group, groupIndex) => {
          const detail = groupDetail(group);
          const drillable = isDrillable(group);
          // Empty when the server sent no start time; the bare "Firing since" label would read as broken.
          const firingSince = formatTimestamp(isoToEpochSeconds(group.first_started_at));
          const content = (
            <>
              <div className="flex min-w-0 flex-col gap-1">
                <div className="flex items-start gap-2">
                  {/* One heading line tall, so the dot centers on the title's first line however the title wraps. */}
                  <span className="flex h-6 shrink-0 items-center">
                    <span className="h-2 w-2 rounded-full bg-intent-warning-fill" />
                  </span>
                  <span className="min-w-0 text-heading-100 text-text-primary">{activeGroupTitle(group)}</span>
                </div>
                <div className="pl-4">
                  {detail ? <div className="text-200 leading-snug text-text-primary-70">{detail}</div> : null}
                  {firingSince ? (
                    <div className="text-200 leading-snug text-text-primary-50">Firing since {firingSince}</div>
                  ) : null}
                </div>
              </div>
              {drillable ? (
                <ChevronDown
                  width={iconSizes.xSmall}
                  className="-rotate-90 text-text-primary-50"
                  testId="alert-drill-in-chevron"
                />
              ) : null}
            </>
          );

          // Both row shapes share the separator so the dividers line up down a mixed list. The padding keeps the
          // hover highlight clear of the text it wraps and of the dividers above and below it.
          const rowClassName = clsx(
            "flex items-center justify-between gap-3 px-3 py-3",
            groupIndex > 0 && "border-t border-border-5",
          );

          return drillable ? (
            <button
              key={group.key}
              type="button"
              onClick={() => onSelectGroup(group)}
              className={clsx(
                rowClassName,
                "rounded-xl text-left transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5",
              )}
            >
              {content}
            </button>
          ) : (
            <div key={group.key} className={rowClassName}>
              {content}
            </div>
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
          Configure alerts
        </Link>
      </div>
    </div>
  );
};

export default ActiveAlertsPopover;
