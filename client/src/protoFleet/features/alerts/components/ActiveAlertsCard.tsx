import { useCallback, useMemo, useState } from "react";
import { useActiveAlertGroups } from "@/protoFleet/features/alerts/api/useActiveAlertGroups";
import { TimestampText } from "@/protoFleet/features/alerts/components/alertColumns";
import AlertInstancesModal from "@/protoFleet/features/alerts/components/AlertInstancesModal";
import StatusDot from "@/protoFleet/features/alerts/components/StatusDot";
import { countLabel } from "@/protoFleet/features/alerts/lib/alertCountLabels";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";

// A device-less rule can fire on several non-device dimensions at once, and the rollup names the rule rather
// than any one of them, so the count is what says the others are firing too.
const calloutTitle = (group: ActiveAlertGroup) =>
  group.alert_count > 1 ? `${group.alert_name} — ${countLabel(group.alert_count, "instance")}` : group.alert_name;

type AlertColumns = "alert" | "miners" | "since";

const alertColTitles: ColTitles<AlertColumns> = {
  alert: "Alert",
  miners: "Affected Miners",
  since: "Firing Since",
};

const alertActiveCols: AlertColumns[] = ["alert", "miners", "since"];

const alertColConfig: ColConfig<ActiveAlertGroup, string, AlertColumns> = {
  alert: {
    component: (group) => <span className="text-emphasis-300 text-text-primary">{group.alert_name}</span>,
    width: "w-72",
    allowWrap: true,
  },
  miners: {
    component: (group) => (
      <StatusDot dotClass="bg-intent-critical-fill">{countLabel(group.device_count, "miner")}</StatusDot>
    ),
    width: "w-44",
  },
  since: {
    component: (group) => <TimestampText iso={group.first_started_at} />,
    width: "w-48",
  },
};

const ActiveAlertsCard = () => {
  const { groups, loading, error, denied, hasMore } = useActiveAlertGroups();
  // The clicked group is captured, not looked up per poll: the drill-in pages a snapshot, and re-deriving it
  // would close the modal (discarding every loaded page) the moment the alert resolved mid-read.
  const [selectedGroup, setSelectedGroup] = useState<ActiveAlertGroup | null>(null);

  // No affected miners means no blast radius to rank or list, so these get a callout rather than a row reading
  // "0 miners". Keyed off the server's count, so no client-side list of device-less rule groups to keep in sync.
  const { calloutAlerts, alertRows } = useMemo(
    () => ({
      calloutAlerts: groups.filter((group) => group.device_count === 0),
      alertRows: groups.filter((group) => group.device_count > 0),
    }),
    [groups],
  );

  const handleClose = useCallback(() => setSelectedGroup(null), []);

  // The dashboard gate is a flat permission union; a site-scoped alert:read grant reaches here but is
  // denied the org-scoped history RPC, so drop the card on that denial rather than poll it forever.
  if (denied) return null;

  const isInitialLoad = loading && groups.length === 0;
  const isEmpty = groups.length === 0;

  return (
    <section className="flex flex-col gap-4 rounded-xl bg-surface-elevated-base p-6 shadow-100">
      <h3 className="text-heading-200">Active alerts</h3>

      {error ? <Callout intent="danger" prefixIcon={<Alert />} title={error} /> : null}

      {isInitialLoad ? (
        <div className="flex justify-center py-10">
          <ProgressCircular indeterminate />
        </div>
      ) : isEmpty ? (
        <div className="py-6 text-center text-text-primary-50">No active alerts.</div>
      ) : (
        <div className="flex flex-col gap-4">
          {calloutAlerts.map((group) => (
            <Callout
              key={group.key}
              intent="warning"
              prefixIcon={<Alert />}
              title={calloutTitle(group)}
              // What each instance actually says lives on the drill-in, so it is offered even for a single one.
              buttonText="View instances"
              buttonOnClick={() => setSelectedGroup(group)}
            />
          ))}
          {alertRows.length ? (
            <List<ActiveAlertGroup, string, AlertColumns>
              items={alertRows}
              itemKey="key"
              activeCols={alertActiveCols}
              colTitles={alertColTitles}
              colConfig={alertColConfig}
              onRowClick={setSelectedGroup}
              noDataElement={null}
            />
          ) : null}
        </div>
      )}

      {hasMore ? (
        <p className="text-center text-200 text-text-primary-50">
          Showing the first {groups.length} alerts; additional firing alerts are not shown.
        </p>
      ) : null}

      {selectedGroup ? <AlertInstancesModal group={selectedGroup} onClose={handleClose} /> : null}
    </section>
  );
};

export default ActiveAlertsCard;
