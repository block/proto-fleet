import type { ReactNode } from "react";

import { usePagedAlerts } from "@/protoFleet/features/alerts/api/usePagedAlerts";
import PagedAlertTable from "@/protoFleet/features/alerts/components/PagedAlertTable";
import type { AlertInstanceColumns } from "@/protoFleet/features/alerts/lib/alertInstanceColumns";
import { alertInstanceColTitles } from "@/protoFleet/features/alerts/lib/alertInstanceColumns";

const activeCols: AlertInstanceColumns[] = ["alert", "status", "device", "mac", "received", "summary"];

// The unfiltered feed: every alert the org has received, firing or resolved.
const historyFilter = {};

interface HistoryTableProps {
  noDataElement: ReactNode;
}

const HistoryTable = ({ noDataElement }: HistoryTableProps) => {
  const { items, loading, error, hasMore, loadMore } = usePagedAlerts(historyFilter, "Failed to load alert history");

  return (
    <PagedAlertTable
      items={items}
      loading={loading}
      error={error}
      hasMore={hasMore}
      onLoadMore={loadMore}
      activeCols={activeCols}
      colTitles={alertInstanceColTitles}
      noDataElement={noDataElement}
    />
  );
};

export default HistoryTable;
