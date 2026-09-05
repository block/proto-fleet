import { useCallback, useMemo, useState } from "react";

import { getComponentIcon, getComponentIconColor } from "../../componentIcons";
import type { TicketItem } from "../../types";
import BulkCloseModal from "../BulkClose/BulkCloseModal";
import CreateTicketModal from "../CreateTicket/CreateTicketModal";
import ListPagination from "../ListPagination";
import TicketDetailModal from "../TicketDetail/TicketDetailModal";
import {
  SortDirection,
  TicketCategory,
  TicketSortField,
  TicketStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import { useTicketQueue } from "@/protoFleet/features/maintenance/hooks/useTicketQueue";
import { useHasPermission } from "@/protoFleet/store";
import { Info } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List, { type SelectionMode } from "@/shared/components/List";
import FilterChipsBar, { type FilterChipsBarFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";
import StatusCircle from "@/shared/components/StatusCircle";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type TicketColumns = "urgent" | "issue" | "asset" | "location" | "status";
export type TicketQueueViewMode = "list" | "kanban";
interface TicketQueueProps {
  initialViewMode?: TicketQueueViewMode;
}

const STATUS_OPTIONS = [
  { id: "open", label: "Open" },
  { id: "in_progress", label: "In Progress" },
  { id: "on_hold", label: "On Hold" },
  { id: "sent_to_vendor", label: "Sent to Vendor" },
];
const CATEGORY_OPTIONS = [
  { id: "miner", label: "Miner" },
  { id: "infrastructure", label: "Infrastructure" },
];
const statusEnums: Record<string, TicketStatus> = {
  open: TicketStatus.OPEN,
  in_progress: TicketStatus.IN_PROGRESS,
  on_hold: TicketStatus.ON_HOLD,
  sent_to_vendor: TicketStatus.SENT_TO_VENDOR,
};
const categoryEnums: Record<string, TicketCategory> = {
  miner: TicketCategory.MINER,
  infrastructure: TicketCategory.INFRASTRUCTURE,
};
const formatStatus = (status: string) =>
  ({
    open: "Open",
    in_progress: "In Progress",
    on_hold: "On Hold",
    sent_to_vendor: "Sent to Vendor",
    completed: "Completed",
  })[status] ?? status;
const statusCircleMap = (status: string) =>
  status === "open"
    ? ("warning" as const)
    : status === "in_progress" || status === "completed"
      ? ("normal" as const)
      : status === "on_hold"
        ? ("sleeping" as const)
        : ("inactive" as const);
const ageText = (createdAt: Date | null) => {
  if (!createdAt) return "";
  const hours = Math.max(0, Math.floor((Date.now() - createdAt.getTime()) / 3_600_000));
  return hours >= 24 ? `${Math.floor(hours / 24)}d ${hours % 24}h` : `${hours}h`;
};
const DESKTOP_COLS: TicketColumns[] = ["urgent", "issue", "asset", "location", "status"];
const PHONE_COLS: TicketColumns[] = ["urgent", "issue", "status"];
const colTitles: ColTitles<TicketColumns> = {
  urgent: "",
  issue: "Issue",
  asset: "Asset",
  location: "Location",
  status: "Status",
};
const sortFields: Record<TicketColumns, TicketSortField> = {
  urgent: TicketSortField.CREATED_AT,
  issue: TicketSortField.COMPONENT,
  asset: TicketSortField.ASSET,
  location: TicketSortField.LOCATION,
  status: TicketSortField.STATUS,
};

const TicketQueue = ({ initialViewMode = "list" }: TicketQueueProps) => {
  const canManage = useHasPermission("maintenance:manage");
  const { isPhone, isTablet } = useWindowDimensions();
  const isCompact = isPhone || isTablet;
  const queue = useTicketQueue({ excludeCompleted: true });
  const options = useMaintenanceOptions();
  const [viewMode, setViewMode] = useState<TicketQueueViewMode>(initialViewMode);
  const [detailTicketId, setDetailTicketId] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showBulkCloseModal, setShowBulkCloseModal] = useState(false);
  const [selectedTicketIds, setSelectedTicketIds] = useState<string[]>([]);
  const [myTicketsActive, setMyTicketsActive] = useState(false);
  const [filters, setFilters] = useState<Record<string, string[]>>({});

  const applyFilters = useCallback(
    (next: Record<string, string[]>, mine = myTicketsActive) => {
      queue.setFilter({
        excludeCompleted: true,
        statuses: (next.status ?? []).map((v) => statusEnums[v]).filter(Boolean),
        categories: (next.category ?? []).map((v) => categoryEnums[v]).filter(Boolean),
        siteIds: (next.site ?? []).map(BigInt),
        assigneeUserId: mine && options.currentAssignee ? BigInt(options.currentAssignee.id) : undefined,
      });
    },
    [myTicketsActive, options.currentAssignee, queue],
  );
  const handleFilter = useCallback(
    (key: string, values: string[]) => {
      const next = { ...filters, [key]: values };
      setFilters(next);
      applyFilters(next);
      setSelectedTicketIds([]);
    },
    [applyFilters, filters],
  );
  const toggleMine = useCallback(() => {
    if (!options.currentAssignee) return;
    const next = !myTicketsActive;
    setMyTicketsActive(next);
    applyFilters(filters, next);
  }, [applyFilters, filters, myTicketsActive, options.currentAssignee]);
  const chipFilters = useMemo<FilterChipsBarFilter[]>(
    () => [
      {
        key: "status",
        title: "Status",
        pluralTitle: "Statuses",
        options: STATUS_OPTIONS,
        selectedValues: filters.status ?? [],
      },
      {
        key: "category",
        title: "Category",
        pluralTitle: "Categories",
        options: CATEGORY_OPTIONS,
        selectedValues: filters.category ?? [],
      },
      {
        key: "site",
        title: "Site",
        options: options.sites.map((site) => ({ id: site.id, label: site.name })),
        selectedValues: filters.site ?? [],
      },
    ],
    [filters, options.sites],
  );

  const colConfig: ColConfig<TicketItem, string, TicketColumns> = useMemo(
    () => ({
      urgent: {
        component: (t) => (
          <div className={`flex items-center justify-center ${getComponentIconColor(t.urgent)}`}>
            {getComponentIcon(t.component, t.urgent)}
          </div>
        ),
        width: isCompact ? "w-8" : "w-10",
      },
      issue: {
        component: (t) => (
          <div className="flex flex-col">
            <span className="text-emphasis-300 font-medium">
              {t.component}: {t.diagnosis}
            </span>
            <span className="text-300 text-text-primary-70">{t.ticketNumber}</span>
          </div>
        ),
        width: "w-64",
      },
      asset: {
        component: (t) => (
          <div className="flex flex-col">
            <span>{t.minerIdentifier ?? t.component}</span>
            <span className="text-300 text-text-primary-70">{t.buildingName ?? "—"}</span>
          </div>
        ),
        width: "w-48",
      },
      location: {
        component: (t) => (
          <div className="flex flex-col">
            <span>{[t.buildingName, t.rackLabel].filter(Boolean).join(", ")}</span>
            <span className="text-300 text-text-primary-70">{[t.siteName, t.zone].filter(Boolean).join(", ")}</span>
          </div>
        ),
        width: "w-48",
      },
      status: {
        component: (t) => (
          <div className="flex items-start gap-2">
            <StatusCircle status={statusCircleMap(t.status)} />
            <div className="flex flex-col">
              <span>{formatStatus(t.status)}</span>
              <span className="text-300 text-text-primary-70">{t.assigneeName ?? "Unassigned"}</span>
            </div>
          </div>
        ),
        width: "w-36",
      },
    }),
    [isCompact],
  );
  const rowActions: ListAction<TicketItem>[] = useMemo(
    () =>
      canManage
        ? [
            { title: "Assign or update", actionHandler: (t) => setDetailTicketId(t.id) },
            {
              title: (t) => (t.urgent ? "Remove urgent" : "Mark urgent"),
              actionHandler: (t) =>
                void (t.urgent
                  ? queue.setUrgent(t.id, false)
                  : queue.bulkUpdate([t.id], { case: "markUrgent", value: true })),
            },
            {
              title: "Close ticket",
              actionHandler: (t) => {
                setSelectedTicketIds([t.id]);
                setShowBulkCloseModal(true);
              },
              variant: "destructive" as const,
              showDividerAfter: false,
            },
          ]
        : [],
    [canManage, queue],
  );
  const statusCounts = useMemo(
    () => ({
      open: queue.stats?.openCount ?? 0,
      in_progress: queue.stats?.inProgressCount ?? 0,
      on_hold: queue.stats?.onHoldCount ?? 0,
      sent_to_vendor: queue.stats?.sentToVendorCount ?? 0,
    }),
    [queue.stats],
  );
  const handleViewMode = useCallback(
    (key: string) => {
      const next = key as TicketQueueViewMode;
      if (next === viewMode) return;
      setViewMode(next);
      setSelectedTicketIds([]);
      void queue.resetPagination();
    },
    [queue, viewMode],
  );
  const renderActionBar = useCallback(
    (selected: string[], clear: () => void, mode: SelectionMode) => (
      <ActionBar
        className="fixed right-0 bottom-4 left-0 z-20 laptop:left-16 desktop:left-50"
        selectedItems={selected}
        selectionMode={mode}
        onClose={clear}
        renderActions={() => (
          <>
            <Button
              text="Mark Urgent"
              variant={variants.secondary}
              size={buttonSizes.compact}
              onClick={() => void queue.bulkUpdate(selected, { case: "markUrgent", value: true })}
            />
            <Button
              text="Close"
              variant={variants.danger}
              size={buttonSizes.compact}
              onClick={() => {
                setSelectedTicketIds(selected);
                setShowBulkCloseModal(true);
              }}
            />
          </>
        )}
      />
    ),
    [queue],
  );

  if (queue.loading && queue.data.length === 0) {
    return (
      <div role="status" aria-label="Loading tickets" className="flex justify-center py-20">
        <ProgressCircular indeterminate />
      </div>
    );
  }
  if (queue.error && queue.data.length === 0)
    return (
      <div role="alert">
        {queue.error}
        <Button text="Retry" variant={variants.secondary} onClick={() => void queue.refresh()} />
      </div>
    );
  return (
    <div className="flex flex-col">
      {(queue.stats?.overdueCount ?? 0) > 0 ? (
        <div className="mb-4 flex items-center gap-3 rounded-xl border border-border-5 px-4 py-3">
          <Info width="w-5" />
          <span>{queue.stats?.overdueCount} tickets overdue</span>
        </div>
      ) : null}
      <div className={`flex gap-2 pb-4 ${isCompact ? "flex-col" : "flex-wrap items-center"}`}>
        <SegmentedControl
          key={viewMode}
          segments={[
            { key: "list", title: "List" },
            { key: "kanban", title: "Board" },
          ]}
          initialSegmentKey={viewMode}
          onSelect={handleViewMode}
        />
        <Button
          variant={myTicketsActive ? variants.accent : variants.ghost}
          size={buttonSizes.compact}
          disabled={!options.currentAssignee}
          onClick={toggleMine}
        >
          My tickets
        </Button>
        <FilterChipsBar filters={chipFilters} onChange={handleFilter} />
        {canManage ? (
          <Button
            className="ml-auto"
            text="Create ticket"
            variant={variants.secondary}
            size={buttonSizes.compact}
            onClick={() => setShowCreateModal(true)}
          />
        ) : null}
      </div>
      {queue.data.length === 0 ? (
        <div>No tickets</div>
      ) : viewMode === "list" ? (
        <List
          items={queue.data}
          itemKey="id"
          activeCols={isCompact ? PHONE_COLS : DESKTOP_COLS}
          colTitles={colTitles}
          colConfig={colConfig}
          actions={rowActions}
          itemSelectable={canManage}
          stickyFirstColumn={false}
          overflowContainer={false}
          total={queue.total}
          hideTotal
          itemName={{ singular: "ticket", plural: "tickets" }}
          sortableColumns={new Set<TicketColumns>(["issue", "asset", "location", "status"])}
          onSort={(field, direction) =>
            queue.setSort(sortFields[field], direction === "asc" ? SortDirection.ASC : SortDirection.DESC)
          }
          onRowClick={(t) => setDetailTicketId(t.id)}
          renderActionBar={canManage ? renderActionBar : undefined}
        />
      ) : (
        <TicketKanbanView tickets={queue.data} counts={statusCounts} onClick={(t) => setDetailTicketId(t.id)} />
      )}
      {viewMode === "list" ? (
        <ListPagination
          currentPage={queue.currentPage}
          pageSize={50}
          visibleCount={queue.data.length}
          total={queue.total}
          itemName="tickets"
          hasNextPage={!!queue.nextPageToken}
          loading={queue.loading}
          onPrevious={() => {
            setSelectedTicketIds([]);
            void queue.previousPage();
          }}
          onNext={() => {
            setSelectedTicketIds([]);
            void queue.nextPage();
          }}
        />
      ) : queue.nextPageToken ? (
        <Button
          text="Load more"
          variant={variants.secondary}
          onClick={() => void queue.loadMore()}
          loading={queue.loading}
        />
      ) : null}
      {detailTicketId ? (
        <TicketDetailModal
          ticketId={detailTicketId}
          ticketIds={queue.data.map((t) => t.id)}
          onDismiss={() => setDetailTicketId(null)}
          onMutationSuccess={() => void queue.refresh()}
        />
      ) : null}
      {showCreateModal ? (
        <CreateTicketModal
          onDismiss={() => setShowCreateModal(false)}
          onSuccess={() => {
            setShowCreateModal(false);
            void queue.refresh();
          }}
        />
      ) : null}
      {showBulkCloseModal ? (
        <BulkCloseModal
          ticketIds={selectedTicketIds}
          includesMiner={queue.data.some((t) => selectedTicketIds.includes(t.id) && t.category === "miner")}
          onDismiss={() => setShowBulkCloseModal(false)}
          onSubmit={(mutation) => queue.bulkUpdate(selectedTicketIds, mutation)}
          onSuccess={() => {
            setShowBulkCloseModal(false);
            setSelectedTicketIds([]);
          }}
        />
      ) : null}
    </div>
  );
};

const KANBAN_COLUMNS = STATUS_OPTIONS;
const TicketKanbanView = ({
  tickets,
  counts,
  onClick,
}: {
  tickets: TicketItem[];
  counts: Record<string, number>;
  onClick: (ticket: TicketItem) => void;
}) => (
  <div className="grid auto-cols-[minmax(260px,1fr)] grid-flow-col gap-4 overflow-x-auto">
    {KANBAN_COLUMNS.map((column) => (
      <div key={column.id}>
        <div className="pb-3 text-300 text-text-primary-70">
          {column.label} ({counts[column.id] ?? 0})
        </div>
        {tickets
          .filter((t) => t.status === column.id)
          .map((ticket) => (
            <button
              key={ticket.id}
              type="button"
              className="mb-2 flex w-full flex-col rounded-xl bg-surface-5 px-5 py-4 text-left"
              onClick={() => onClick(ticket)}
            >
              <span>{ticket.ticketNumber}</span>
              <span>{ticket.diagnosis}</span>
              <span className="text-200 text-text-primary-70">
                {[ticket.assigneeName ?? "Unassigned", ageText(ticket.createdAt)].filter(Boolean).join(", ")}
              </span>
            </button>
          ))}
      </div>
    ))}
  </div>
);
export default TicketQueue;
