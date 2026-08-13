import { Link } from "react-router-dom";
import clsx from "clsx";

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

// The rollup names the rule rather than any one dimension a device-less rule fired on, so the count reports the rest.
const groupTitle = (group: ActiveAlertGroup) =>
  group.device_count === 0 && group.alert_count > 1
    ? `${group.alert_name} — ${countLabel(group.alert_count, "instance")}`
    : group.alert_name;

// Affected miners are the blast radius worth leading with; a rule firing on none has no drill-in to open, so the
// server sends its summary instead and the row says what fired inline.
const groupDetail = (group: ActiveAlertGroup) =>
  group.device_count > 0 ? `${countLabel(group.device_count, "miner")} affected` : group.summary;

// Only the affected miners are worth a drill-in: it lists one row per miner, and a device-less rule has none.
const isDrillable = (group: ActiveAlertGroup) => group.device_count > 0;

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
                  <span className="min-w-0 text-heading-100 text-text-primary">{groupTitle(group)}</span>
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
