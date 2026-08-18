import { create } from "@bufbuild/protobuf";
import { timestampFromMs, timestampMs } from "@bufbuild/protobuf/wkt";

import {
  type ActivityEntry,
  ActivityEntrySchema,
  type ActivityFilter,
  type EventTypeOption,
  EventTypeOptionSchema,
} from "@/protoFleet/api/generated/activity/v1/activity_pb";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";

// Pseudo event type for the "Alerts" filter option; unknown to the server, so it matches no activities.
export const ALERT_EVENT_TYPE = "alert";

// The synthetic event types every consumer (icons, formatter, modal) keys on.
export const ALERT_FIRING_EVENT_TYPE = "alert_firing";
export const ALERT_RESOLVED_EVENT_TYPE = "alert_resolved";

// Alerts have no user or searchable description, so either filter excludes the feed wholesale;
// scope is matched per entry (alertEntryMatchesScopes) since device alerts carry a device scope.
export const alertsMatchFilter = (filter: ActivityFilter): boolean =>
  filter.userIds.length === 0 &&
  filter.searchText === "" &&
  (filter.eventTypes.length === 0 || filter.eventTypes.includes(ALERT_EVENT_TYPE));

export const alertEntryMatchesScopes = (entry: ActivityEntry, filter: ActivityFilter): boolean =>
  filter.scopeTypes.length === 0 || (entry.scopeType !== undefined && filter.scopeTypes.includes(entry.scopeType));

export const ALERT_TYPE_OPTION: EventTypeOption = create(EventTypeOptionSchema, {
  eventType: ALERT_EVENT_TYPE,
  eventCategory: "alert",
});

// The eventId prefix disambiguates synthetic rows should the server ever emit a real "alert" category.
export const isAlertEntry = (entry: ActivityEntry): boolean =>
  entry.eventCategory === "alert" && entry.eventId.startsWith("alert-");

// Synthetic entries let alert rows reuse the whole activity rendering pipeline (table, icons, modal).
export const activityEntryFromAlert = (alert: AlertHistoryEntry): ActivityEntry => {
  const resolved = alert.status === "resolved";
  // A malformed received_at degrades to an undated entry; timestampFromMs(NaN) would throw in render.
  const receivedMs = Date.parse(alert.received_at);
  return create(ActivityEntrySchema, {
    eventId: `alert-${alert.id}`,
    eventCategory: "alert",
    eventType: resolved ? ALERT_RESOLVED_EVENT_TYPE : ALERT_FIRING_EVENT_TYPE,
    description: `Alert ${resolved ? "resolved" : "firing"}: ${alert.alert_name}`,
    scopeType: alert.device_name ? "device" : undefined,
    scopeLabel: alert.device_name || undefined,
    actorType: "system",
    result: "success",
    createdAt: Number.isFinite(receivedMs) ? timestampFromMs(receivedMs) : undefined,
    metadata: { severity: alert.severity, summary: alert.summary, mac: alert.device_mac },
  });
};

// The decode side of activityEntryFromAlert's metadata, kept beside the encoder so the keys stay paired.
export const alertEntryMetadata = (entry: ActivityEntry) => {
  const str = (key: string): string => {
    const value = entry.metadata?.[key];
    return typeof value === "string" ? value : "";
  };
  return { severity: str("severity"), summary: str("summary"), mac: str("mac") };
};

const entryTimeMs = (entry: ActivityEntry): number => (entry.createdAt ? timestampMs(entry.createdAt) : 0);

// A two-pointer interleave that keeps each feed in its own pagination order (the alert cursor pages by
// id, which timestamps only approximate) and pauses at an unexhausted feed's end, so previously shown
// rows never move and "load more" only appends. An empty feed that still has pages (its first request
// hasn't answered) holds everything back for the same reason: rendering rows its response may predate
// would show entries only to remove them.
export function mergeAlertEntries(
  activities: ActivityEntry[],
  activitiesHaveMore: boolean,
  alertEntries: ActivityEntry[],
  alertsHaveMore: boolean,
): ActivityEntry[] {
  if (alertEntries.length === 0 && !alertsHaveMore) return activities;
  const merged: ActivityEntry[] = [];
  let i = 0;
  let j = 0;
  while (i < activities.length || j < alertEntries.length) {
    if (i === activities.length) {
      if (activitiesHaveMore) break;
      merged.push(alertEntries[j++]);
    } else if (j === alertEntries.length) {
      if (alertsHaveMore) break;
      merged.push(activities[i++]);
    } else {
      merged.push(entryTimeMs(activities[i]) >= entryTimeMs(alertEntries[j]) ? activities[i++] : alertEntries[j++]);
    }
  }
  return merged;
}
