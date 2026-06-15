import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useNotificationsStore } from "@/protoFleet/features/notifications/store/notificationsStore";
import type { NotificationHistoryEntry } from "@/protoFleet/features/notifications/types";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";

type HistoryColumns = "alert" | "status" | "device" | "mac" | "received" | "summary";

const colTitles: ColTitles<HistoryColumns> = {
  alert: "Alert",
  status: "Status",
  device: "Device Name",
  mac: "MAC Address",
  received: "Received",
  summary: "Summary",
};

const activeCols: HistoryColumns[] = ["alert", "status", "device", "mac", "received", "summary"];

const StatusBadge = ({ status }: { status: NotificationHistoryEntry["status"] }) => (
  <span className="inline-flex items-center gap-2 text-300 text-text-primary-50">
    <span
      className={clsx(
        "h-2 w-2 rounded-full",
        status === "resolved" ? "bg-intent-success-fill" : "bg-intent-critical-fill",
      )}
    />
    {status === "resolved" ? "Resolved" : "Firing"}
  </span>
);

const colConfig: ColConfig<NotificationHistoryEntry, string, HistoryColumns> = {
  alert: {
    component: (entry) => (
      <span className="flex items-center gap-2">
        <span className="text-emphasis-300 text-text-primary">{entry.alert_name}</span>
        {entry.severity ? (
          <span className="rounded bg-surface-5 px-2 py-0.5 text-200 text-text-primary-50">{entry.severity}</span>
        ) : null}
      </span>
    ),
    width: "w-64",
  },
  status: {
    component: (entry) => <StatusBadge status={entry.status} />,
    width: "w-32",
  },
  device: {
    component: (entry) => <span className="text-text-primary-50">{entry.device_name || "—"}</span>,
    width: "w-48",
  },
  mac: {
    component: (entry) => <span className="text-text-primary-50">{entry.device_mac || "—"}</span>,
    width: "w-44",
  },
  received: {
    component: (entry) => <span className="text-text-primary-50">{new Date(entry.received_at).toLocaleString()}</span>,
    width: "w-48",
  },
  summary: {
    component: (entry) => <span className="text-text-primary-50">{entry.summary || "—"}</span>,
    width: "w-80",
    allowWrap: true,
  },
};

// Dedupes newest-first history to one firing row per fingerprint; alerts older than the loaded window won't surface.
const selectActive = (history: NotificationHistoryEntry[]): NotificationHistoryEntry[] => {
  const latestByKey = new Map<string, NotificationHistoryEntry>();
  for (const entry of history) {
    const key = entry.fingerprint || entry.alert_name;
    if (!latestByKey.has(key)) latestByKey.set(key, entry);
  }
  return [...latestByKey.values()].filter((entry) => entry.status === "firing");
};

interface HistoryTableProps {
  // Collapse to firing-only rows per fingerprint and hide load-more (active view is derived, not paginated).
  activeOnly?: boolean;
  noDataElement: ReactNode;
}

const HistoryTable = ({ activeOnly = false, noDataElement }: HistoryTableProps) => {
  const history = useNotificationsStore((s) => s.history);
  const historyHasMore = useNotificationsStore((s) => s.historyHasMore);
  const historyLoading = useNotificationsStore((s) => s.historyLoading);
  const refreshHistory = useNotificationsStore((s) => s.refreshHistory);
  const loadMoreHistory = useNotificationsStore((s) => s.loadMoreHistory);

  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void refreshHistory().catch((err: unknown) => {
      setError(getErrorMessage(err, "Failed to load notification history"));
    });
  }, [refreshHistory]);

  const handleLoadMore = useMemo(
    () => () => {
      void loadMoreHistory().catch((err: unknown) => {
        setError(getErrorMessage(err, "Failed to load more notifications"));
      });
    },
    [loadMoreHistory],
  );

  const entries = useMemo(() => (activeOnly ? selectActive(history) : history), [activeOnly, history]);

  const isInitialLoad = historyLoading && history.length === 0;
  const isLoadingMore = historyLoading && history.length > 0;

  return (
    <>
      {error ? <Callout intent="danger" prefixIcon={<Alert />} title={error} /> : null}

      {isInitialLoad ? (
        <div className="flex justify-center py-10">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <List<NotificationHistoryEntry, string, HistoryColumns>
          items={entries}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          noDataElement={noDataElement}
        />
      )}

      {!activeOnly && historyHasMore ? (
        <div className="flex justify-center">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            onClick={handleLoadMore}
            loading={isLoadingMore}
            disabled={isLoadingMore}
          >
            Load more
          </Button>
        </div>
      ) : null}
    </>
  );
};

export default HistoryTable;
