import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { Code, ConnectError } from "@connectrpc/connect";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useNotificationHistory } from "@/protoFleet/features/notifications/api/useNotificationHistory";
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

// The active endpoint already returns the latest row per alert instance, so only guard on status here.
// (Don't re-dedup client-side: device_id is redacted without miner:read, which would collapse distinct instances.)
const selectActive = (history: NotificationHistoryEntry[]): NotificationHistoryEntry[] =>
  history.filter((entry) => entry.status === "firing");

interface HistoryTableProps {
  activeOnly?: boolean;
  noDataElement: ReactNode;
  // Called when the active-only RPC is denied at org scope, so the dashboard card can suppress itself.
  onPermissionDenied?: () => void;
}

const HistoryTable = ({ activeOnly = false, noDataElement, onPermissionDenied }: HistoryTableProps) => {
  const { history, historyHasMore, historyLoading, refreshHistory, loadMoreHistory } =
    useNotificationHistory(activeOnly);

  const [error, setError] = useState<string | null>(null);
  const refreshingRef = useRef(false);

  useEffect(() => {
    const load = () => {
      // Skip overlapping polls so a slow response can't resolve after a newer one and overwrite the active set with stale data.
      if (refreshingRef.current) return;
      refreshingRef.current = true;
      void refreshHistory()
        .catch((err: unknown) => {
          // A site-scoped notification:read grant clears the dashboard's flat permission gate but is denied
          // this org-scoped RPC; suppress the card instead of surfacing an error and polling on indefinitely.
          if (activeOnly && err instanceof ConnectError && err.code === Code.PermissionDenied) {
            onPermissionDenied?.();
            return;
          }
          setError(getErrorMessage(err, "Failed to load notification history"));
        })
        .finally(() => {
          refreshingRef.current = false;
        });
    };
    load();
    // The active card lives on the always-open dashboard, so poll it like the other panels; the paginated list refreshes on mount only.
    if (!activeOnly) return;
    const interval = setInterval(load, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [activeOnly, refreshHistory, onPermissionDenied]);

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

      {activeOnly && historyHasMore ? (
        <p className="text-center text-200 text-text-primary-50">
          Showing the first {entries.length} active alerts; additional firing alerts are not shown.
        </p>
      ) : null}
    </>
  );
};

export default HistoryTable;
