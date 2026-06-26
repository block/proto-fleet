import { useCallback, useMemo, useState } from "react";
import { useActiveAlerts } from "@/protoFleet/features/alerts/api/useActiveAlerts";
import {
  AlertNameCell,
  ReceivedCell,
  StatusBadge,
  SummaryCell,
} from "@/protoFleet/features/alerts/components/alertColumns";
import StatusDot from "@/protoFleet/features/alerts/components/StatusDot";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { sizes } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

// The Metric Ingest Stalled rule (uid protofleet-ingest-stalled) is the lone member of the operator-only
// proto-fleet-self group and emits a single fleet-wide instance with no device, so it gets its own row
// rather than landing in the per-miner rollup. Match the stable rule group + absence of a device, not the
// mutable display name (a miner-scoped alert could otherwise share the name and lose its device context).
const FLEET_SELF_RULE_GROUP = "proto-fleet-self";
const isFleetWideAlert = (alert: AlertHistoryEntry) => alert.rule_group === FLEET_SELF_RULE_GROUP && !alert.device_id;

interface MinerAlertGroup {
  deviceId: string;
  deviceName: string;
  deviceMac: string;
  alerts: AlertHistoryEntry[];
  // Distinct alert names, precomputed so the row preview doesn't rebuild a Set on every render.
  alertNames: string;
}

// Roll the firing set up per miner, then order by alert count so the worst-off devices surface first.
const groupByMiner = (alerts: AlertHistoryEntry[]): MinerAlertGroup[] => {
  const groups = new Map<string, MinerAlertGroup>();
  for (const alert of alerts) {
    // Fall back to MAC/name, then to the alert's own fingerprint/id: when a grant can read alerts but
    // not miners the device fields are redacted to "", and we must not collapse every alert into one row.
    const key = alert.device_id || alert.device_mac || alert.device_name || alert.fingerprint || alert.id;
    let group = groups.get(key);
    if (!group) {
      group = { deviceId: key, deviceName: alert.device_name, deviceMac: alert.device_mac, alerts: [], alertNames: "" };
      groups.set(key, group);
    }
    group.alerts.push(alert);
  }
  const result = [...groups.values()];
  for (const group of result) {
    group.alertNames = [...new Set(group.alerts.map((alert) => alert.alert_name))].join(", ");
  }
  return result.sort((a, b) => b.alerts.length - a.alerts.length || a.deviceName.localeCompare(b.deviceName));
};

type MinerColumns = "device" | "mac" | "alerts";

const minerColTitles: ColTitles<MinerColumns> = {
  device: "Device Name",
  mac: "MAC Address",
  alerts: "Active Alerts",
};

const minerActiveCols: MinerColumns[] = ["device", "mac", "alerts"];

const minerColConfig: ColConfig<MinerAlertGroup, string, MinerColumns> = {
  device: {
    component: (group) => <span className="text-emphasis-300 text-text-primary">{group.deviceName || "—"}</span>,
    width: "w-64",
  },
  mac: {
    component: (group) => <span className="text-text-primary-50">{group.deviceMac || "—"}</span>,
    width: "w-44",
  },
  alerts: {
    component: (group) => (
      <span className="flex flex-col gap-0.5">
        <StatusDot dotClass="bg-intent-critical-fill">
          {group.alerts.length} active {group.alerts.length === 1 ? "alert" : "alerts"}
        </StatusDot>
        <span className="text-200 text-text-primary-50">{group.alertNames}</span>
      </span>
    ),
    width: "w-96",
    allowWrap: true,
  },
};

type AlertColumns = "alert" | "status" | "received" | "summary";

const alertColTitles: ColTitles<AlertColumns> = {
  alert: "Alert",
  status: "Status",
  received: "Received",
  summary: "Summary",
};

const alertActiveCols: AlertColumns[] = ["alert", "status", "received", "summary"];

const alertColConfig: ColConfig<AlertHistoryEntry, string, AlertColumns> = {
  alert: { component: AlertNameCell, width: "w-64" },
  status: { component: (entry) => <StatusBadge status={entry.status} />, width: "w-32" },
  received: { component: ReceivedCell, width: "w-48" },
  summary: { component: SummaryCell, width: "w-80", allowWrap: true },
};

const ActiveAlertsCard = () => {
  const { alerts, loading, error, denied, hasMore } = useActiveAlerts();
  const [selectedDeviceId, setSelectedDeviceId] = useState<string | null>(null);

  // At most one fleet-wide (proto-fleet-self) alert is ever active; surface it as a callout above the
  // per-miner rollup rather than as a device row.
  const fleetWideAlert = useMemo(() => alerts.find(isFleetWideAlert) ?? null, [alerts]);
  const groups = useMemo(() => groupByMiner(alerts.filter((alert) => !isFleetWideAlert(alert))), [alerts]);
  const selectedGroup = useMemo(
    () => groups.find((group) => group.deviceId === selectedDeviceId) ?? null,
    [groups, selectedDeviceId],
  );

  const handleRowClick = useCallback((group: MinerAlertGroup) => setSelectedDeviceId(group.deviceId), []);
  const handleClose = useCallback(() => setSelectedDeviceId(null), []);

  // The dashboard gate is a flat permission union; a site-scoped alert:read grant reaches here but is
  // denied the org-scoped history RPC, so drop the card on that denial rather than poll it forever.
  if (denied) return null;

  const isInitialLoad = loading && alerts.length === 0;
  const isEmpty = groups.length === 0 && !fleetWideAlert;

  return (
    <section className="flex flex-col gap-4 rounded-xl bg-surface-base p-6 dark:bg-core-primary-5">
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
          {fleetWideAlert ? (
            <Callout
              intent={fleetWideAlert.severity === "critical" ? "danger" : "warning"}
              prefixIcon={<Alert />}
              title={fleetWideAlert.alert_name}
              subtitle={fleetWideAlert.summary}
            />
          ) : null}
          {groups.length ? (
            <List<MinerAlertGroup, string, MinerColumns>
              items={groups}
              itemKey="deviceId"
              activeCols={minerActiveCols}
              colTitles={minerColTitles}
              colConfig={minerColConfig}
              onRowClick={handleRowClick}
              noDataElement={null}
            />
          ) : null}
        </div>
      )}

      {hasMore ? (
        <p className="text-center text-200 text-text-primary-50">
          Showing the first {alerts.length} active alerts; additional firing alerts are not shown.
        </p>
      ) : null}

      {selectedGroup ? (
        <Modal
          open
          size={sizes.large}
          title={`${selectedGroup.deviceName || "Device"} alerts`}
          onDismiss={handleClose}
          buttons={[{ text: "Done", variant: variants.primary, onClick: handleClose }]}
        >
          <List<AlertHistoryEntry, string, AlertColumns>
            items={selectedGroup.alerts}
            itemKey="id"
            activeCols={alertActiveCols}
            colTitles={alertColTitles}
            colConfig={alertColConfig}
            noDataElement={null}
          />
        </Modal>
      ) : null}
    </section>
  );
};

export default ActiveAlertsCard;
