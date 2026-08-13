import {
  AlertNameCell,
  DeviceMacCell,
  DeviceNameCell,
  ReceivedCell,
  StatusCell,
  SummaryCell,
} from "@/protoFleet/features/alerts/components/alertColumns";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";

export type AlertInstanceColumns = "alert" | "status" | "device" | "mac" | "received" | "summary";

// One config for every per-instance alert table, each picking its subset via activeCols and overriding only the
// titles its context reads differently, so a cell, width or title change lands in all of them at once.
export const alertInstanceColTitles: ColTitles<AlertInstanceColumns> = {
  alert: "Alert",
  status: "Status",
  device: "Device Name",
  mac: "MAC Address",
  received: "Received",
  summary: "Summary",
};

export const alertInstanceColConfig: ColConfig<AlertHistoryEntry, string, AlertInstanceColumns> = {
  alert: { component: AlertNameCell, width: "w-64" },
  status: { component: StatusCell, width: "w-32" },
  device: { component: DeviceNameCell, width: "w-48" },
  mac: { component: DeviceMacCell, width: "w-44" },
  received: { component: ReceivedCell, width: "w-48" },
  summary: { component: SummaryCell, width: "w-80", allowWrap: true },
};
