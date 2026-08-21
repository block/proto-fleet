import type { ReactNode } from "react";

import type { AlertInstanceColumns } from "@/protoFleet/features/alerts/lib/alertInstanceColumns";
import { alertInstanceColConfig } from "@/protoFleet/features/alerts/lib/alertInstanceColumns";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import List from "@/shared/components/List";
import type { ColTitles } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface PagedAlertTableProps {
  items: AlertHistoryEntry[];
  loading: boolean;
  error: string | null;
  hasMore: boolean;
  onLoadMore: () => void;
  activeCols: AlertInstanceColumns[];
  colTitles: ColTitles<AlertInstanceColumns>;
  noDataElement: ReactNode;
  // The surface behind the table: the sticky column paints it opaquely, and in dark mode the page and an
  // elevated panel are different greys, so a table in a modal reads as a mismatched band without this.
  stickyBgColor?: string;
}

// The keyset-paged table behind both per-instance views, the history feed and the drill-in: each supplies its
// own columns and empty state, but their loading, error and load-more states are identical.
const PagedAlertTable = ({
  items,
  loading,
  error,
  hasMore,
  onLoadMore,
  activeCols,
  colTitles,
  noDataElement,
  stickyBgColor,
}: PagedAlertTableProps) => {
  const isLoadingMore = loading && items.length > 0;

  return (
    <>
      {error ? <Callout intent="danger" prefixIcon={<Alert />} title={error} /> : null}

      {loading && items.length === 0 ? (
        <div className="flex justify-center py-10">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <List<AlertHistoryEntry, string, AlertInstanceColumns>
          items={items}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={alertInstanceColConfig}
          noDataElement={noDataElement}
          stickyBgColor={stickyBgColor}
        />
      )}

      {hasMore ? (
        <div className="flex justify-center">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Load more"
            onClick={onLoadMore}
            loading={isLoadingMore}
            disabled={isLoadingMore}
          />
        </div>
      ) : null}
    </>
  );
};

export default PagedAlertTable;
