import type { ReactNode } from "react";
import { useCallback, useEffect, useState } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAlertHistory } from "@/protoFleet/features/alerts/api/useAlertHistory";
import {
  AlertNameCell,
  ReceivedCell,
  StatusBadge,
  SummaryCell,
} from "@/protoFleet/features/alerts/components/alertColumns";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
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

const colConfig: ColConfig<AlertHistoryEntry, string, HistoryColumns> = {
  alert: {
    component: AlertNameCell,
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
    component: ReceivedCell,
    width: "w-48",
  },
  summary: {
    component: SummaryCell,
    width: "w-80",
    allowWrap: true,
  },
};

interface HistoryTableProps {
  noDataElement: ReactNode;
}

const HistoryTable = ({ noDataElement }: HistoryTableProps) => {
  const { history, historyHasMore, historyLoading, refreshHistory, loadMoreHistory } = useAlertHistory();

  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void refreshHistory().catch((err: unknown) => {
      setError(getErrorMessage(err, "Failed to load alert history"));
    });
  }, [refreshHistory]);

  const handleLoadMore = useCallback(() => {
    void loadMoreHistory().catch((err: unknown) => {
      setError(getErrorMessage(err, "Failed to load more alerts"));
    });
  }, [loadMoreHistory]);

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
        <List<AlertHistoryEntry, string, HistoryColumns>
          items={history}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          noDataElement={noDataElement}
        />
      )}

      {historyHasMore ? (
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
