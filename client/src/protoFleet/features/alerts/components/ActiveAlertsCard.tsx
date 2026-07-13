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

// Fleet-wide alerts carry no device: the operator-only proto-fleet-self group (Metric Ingest Stalled)
// and the host-scoped proto-fleet-system rules (CPU/memory/disk/heartbeat). They get callouts above the
// per-miner rollup rather than landing in it as "—" device rows. Match the stable rule group + absence
// of a device, not the mutable display name (a miner-scoped alert could otherwise share the name and
// lose its device context).
const FLEET_WIDE_RULE_GROUPS = new Set(["proto-fleet-self", "proto-fleet-system"]);
const isFleetWideAlert = (alert: AlertHistoryEntry) => FLEET_WIDE_RULE_GROUPS.has(alert.rule_group) && !alert.device_id;

// MQTT curtailment alerts are source-scoped, not device-scoped, so they get callouts instead of
// per-miner rows. Match the stable template values + absence of a device, not display names, so a
// device alert with redacted device fields can never be misclassified as source-level.
const SOURCE_ALERT_TEMPLATES = new Set(["mqtt-curtailment", "mqtt-disconnected"]);
const isSourceAlert = (alert: AlertHistoryEntry) => SOURCE_ALERT_TEMPLATES.has(alert.template) && !alert.device_id;

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

  const fleetWideAlerts = useMemo(() => alerts.filter(isFleetWideAlert), [alerts]);
  const sourceAlerts = useMemo(() => alerts.filter(isSourceAlert), [alerts]);
  const groups = useMemo(
    () => groupByMiner(alerts.filter((alert) => !isFleetWideAlert(alert) && !isSourceAlert(alert))),
    [alerts],
  );
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
  const isEmpty = groups.length === 0 && fleetWideAlerts.length === 0 && sourceAlerts.length === 0;

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
          {fleetWideAlerts.map((alert) => (
            <Callout
              key={alert.id}
              intent={alert.severity === "critical" ? "danger" : "warning"}
              prefixIcon={<Alert />}
              title={alert.alert_name}
              subtitle={alert.summary}
            />
          ))}
          {sourceAlerts.map((alert) => (
            <Callout
              key={alert.id}
              intent={alert.severity === "critical" ? "danger" : "warning"}
              prefixIcon={<Alert />}
              title={alert.alert_name}
              subtitle={alert.summary}
            />
          ))}
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
