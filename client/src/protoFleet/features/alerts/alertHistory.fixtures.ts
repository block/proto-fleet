import { EMPTY_PAGED_ALERTS, type UsePagedAlertsResult } from "@/protoFleet/features/alerts/api/usePagedAlerts";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";

export const buildPagedAlertsResult = (overrides: Partial<UsePagedAlertsResult> = {}): UsePagedAlertsResult => ({
  ...EMPTY_PAGED_ALERTS,
  ...overrides,
});

export const buildAlertHistoryEntry = (overrides: Partial<AlertHistoryEntry> = {}): AlertHistoryEntry => ({
  id: "1",
  received_at: "2026-08-01T00:00:00Z",
  alert_name: "Miner Offline",
  status: "firing",
  severity: "critical",
  rule_group: "",
  fingerprint: "fp",
  device_id: "d1",
  device_name: "miner-1",
  device_mac: "aa:bb:cc",
  template: "offline",
  summary: "miner-1 has been offline for 5m",
  starts_at: null,
  ends_at: null,
  ...overrides,
});
