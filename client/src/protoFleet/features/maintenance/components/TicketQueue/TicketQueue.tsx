import { useCallback, useMemo, useState } from "react";

import TicketDetailModal from "../TicketDetail/TicketDetailModal";
import CreateTicketModal from "../CreateTicket/CreateTicketModal";
import BulkCloseModal from "../BulkClose/BulkCloseModal";
import { mockTickets, CURRENT_USER } from "../../mockData";
import { Alert, Dismiss, Info } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";
import StatusCircle from "@/shared/components/StatusCircle";
import { getComponentIcon, getComponentIconColor } from "../../componentIcons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List, { type SelectionMode } from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import FilterChipsBar, { type FilterChipsBarFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import SegmentedControl from "@/shared/components/SegmentedControl";

type TicketColumns = "urgent" | "issue" | "asset" | "location" | "status";

interface TicketItem {
  id: string;
  ticketNumber: string;
  category: string;
  status: string;
  urgent: boolean;
  component: string;
  diagnosis: string;
  minerIdentifier: string | null;
  minerType: string | null;
  assigneeName: string | null;
  siteName: string;
  buildingName: string;
  rackLabel: string;
  zone: string;
  groupLabel: string;
  commentCount: number;
  partsCount: number;
  age: string;
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

const statusCircleMap = (status: string) => {
  switch (status) {
    case "open":
      return "warning" as const;
    case "in_progress":
      return "normal" as const;
    case "on_hold":
      return "sleeping" as const;
    case "sent_to_vendor":
      return "inactive" as const;
    case "completed":
      return "normal" as const;
    default:
      return "inactive" as const;
  }
};

const formatStatus = (status: string) => {
  switch (status) {
    case "open":
      return "Open";
    case "in_progress":
      return "In Progress";
    case "on_hold":
      return "On Hold";
    case "sent_to_vendor":
      return "Sent to Vendor";
    case "completed":
      return "Completed";
    default:
      return status;
  }
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

const TicketQueue = () => {
  const { isPhone, isTablet } = useWindowDimensions();
  const isCompact = isPhone || isTablet;
  const [tickets] = useState<TicketItem[]>(mockTickets);
  const [viewMode, setViewMode] = useState("list");
  const [detailTicketId, setDetailTicketId] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showBulkCloseModal, setShowBulkCloseModal] = useState(false);
  const [selectedTicketIds, setSelectedTicketIds] = useState<string[]>([]);

  const colConfig: ColConfig<TicketItem, string, TicketColumns> = useMemo(
    () => ({
      urgent: {
        component: (ticket) => (
          <div className={`flex items-center justify-center ${getComponentIconColor(ticket.urgent)}`}>
            {getComponentIcon(ticket.component, ticket.urgent)}
          </div>
        ),
        width: isCompact ? "w-8" : "w-10",
      },
      issue: {
        component: (ticket) => (
          <div className="flex flex-col">
            <span className="text-emphasis-300 font-medium">
              {ticket.component}: {ticket.diagnosis}
            </span>
            <span className="text-300 text-text-primary-70">{ticket.ticketNumber}</span>
          </div>
        ),
      },
      asset: {
        component: (ticket) => (
          <div className="flex flex-col">
            <span className="text-emphasis-300 font-medium">
              {ticket.minerIdentifier ?? ticket.component}
            </span>
            <span className="text-300 text-text-primary-70">
              {ticket.minerType ?? ticket.buildingName}
            </span>
          </div>
        ),
      },
      location: {
        component: (ticket) => (
          <div className="flex flex-col">
            <span className="text-300">
              {ticket.buildingName}
              {ticket.rackLabel ? `, ${ticket.rackLabel}` : ""}
            </span>
            <span className="text-300 text-text-primary-70">
              {ticket.siteName}
              {ticket.zone ? `, ${ticket.zone}` : ""}
            </span>
          </div>
        ),
      },
      status: {
        component: (ticket) => (
          <div className="flex items-start gap-2">
            <div className="mt-1">
              <StatusCircle status={statusCircleMap(ticket.status)} />
            </div>
            <div className="flex flex-col">
              <span className="text-emphasis-300 font-medium">{formatStatus(ticket.status)}</span>
              <span className="text-300 text-text-primary-70">{ticket.assigneeName ?? "Unassigned"}</span>
            </div>
          </div>
        ),
      },
    }),
    [isCompact],
  );

  const [myTicketsActive, setMyTicketsActive] = useState(false);
  const [overdueDismissed, setOverdueDismissed] = useState(false);
  const [activeDropdownFilters, setActiveDropdownFilters] = useState<Record<string, string[]>>({});

  const chipFilters: FilterChipsBarFilter[] = useMemo(
    () => [
      {
        key: "status",
        title: "Status",
        pluralTitle: "Statuses",
        options: STATUS_OPTIONS.map((o) => ({ id: o.id, label: o.label })),
        selectedValues: activeDropdownFilters["status"] ?? [],
      },
      {
        key: "category",
        title: "Category",
        pluralTitle: "Categories",
        options: CATEGORY_OPTIONS.map((o) => ({ id: o.id, label: o.label })),
        selectedValues: activeDropdownFilters["category"] ?? [],
      },
      {
        key: "site",
        title: "Site",
        options: [...new Set(tickets.map((t) => t.siteName))].sort().map((s) => ({ id: s, label: s })),
        selectedValues: activeDropdownFilters["site"] ?? [],
      },
      {
        key: "building",
        title: "Building",
        options: [...new Set(tickets.map((t) => t.buildingName))].sort().map((b) => ({ id: b, label: b })),
        selectedValues: activeDropdownFilters["building"] ?? [],
      },
    ],
    [tickets, activeDropdownFilters],
  );

  const handleChipFilterChange = useCallback((key: string, values: string[]) => {
    setActiveDropdownFilters((prev) => ({ ...prev, [key]: values }));
  }, []);

  const rowActions: ListAction<TicketItem>[] = useMemo(
    () => [
      {
        title: "Assign",
        actionHandler: (ticket) => setDetailTicketId(ticket.id),
        hidden: (ticket) => !!ticket.assigneeName,
      },
      {
        title: "Update status",
        actionHandler: (ticket) => setDetailTicketId(ticket.id),
      },
      {
        title: (ticket) => (ticket.urgent ? "Remove urgent" : "Mark urgent"),
        actionHandler: () => {},
      },
      {
        title: "Close ticket",
        actionHandler: (ticket) => {
          setSelectedTicketIds([ticket.id]);
          setShowBulkCloseModal(true);
        },
        variant: "destructive" as const,
        showDividerAfter: false,
      },
    ],
    [],
  );

  const filterTicket = useCallback(
    (ticket: TicketItem) => {
      if (myTicketsActive && ticket.assigneeName !== CURRENT_USER) return false;
      const statusF = activeDropdownFilters["status"];
      if (statusF?.length && !statusF.includes(ticket.status)) return false;
      const categoryF = activeDropdownFilters["category"];
      if (categoryF?.length && !categoryF.includes(ticket.category)) return false;
      const siteF = activeDropdownFilters["site"];
      if (siteF?.length && !siteF.includes(ticket.siteName)) return false;
      const buildingF = activeDropdownFilters["building"];
      if (buildingF?.length && !buildingF.includes(ticket.buildingName)) return false;
      return true;
    },
    [myTicketsActive, activeDropdownFilters],
  );

  const filteredTickets = useMemo(
    () => tickets.filter(filterTicket),
    [tickets, filterTicket],
  );

  const handleRowClick = useCallback((ticket: TicketItem) => {
    setDetailTicketId(ticket.id);
  }, []);

  const renderActionBar = useCallback(
    (selected: string[], clearSelection: () => void, selectionMode: SelectionMode) => (
      <ActionBar
        className="fixed right-0 bottom-4 left-0 z-20 laptop:left-16 desktop:left-50"
        selectedItems={selected}
        selectionMode={selectionMode}
        onClose={clearSelection}
        renderActions={() => (
          <>
            <Button
              className="bg-grayscale-white-10! text-grayscale-white-90!"
              text="Assign"
              variant={variants.secondary}
              size={buttonSizes.compact}
            />
            <Button
              className="bg-grayscale-white-10! text-grayscale-white-90!"
              text="Update Status"
              variant={variants.secondary}
              size={buttonSizes.compact}
            />
            <Button
              className="bg-grayscale-white-10! text-grayscale-white-90!"
              text="Mark Urgent"
              variant={variants.secondary}
              size={buttonSizes.compact}
            />
            <Button
              className="bg-grayscale-white-10! text-grayscale-white-90!"
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
    [],
  );

  const queueStats = useMemo(() => {
    const active = tickets.filter((t) => t.status !== "completed");
    const overdue = active.filter((t) => t.age.includes("d") && parseInt(t.age) >= 3).length;
    return { overdue };
  }, [tickets]);

  const statusCounts = useMemo(() => {
    const counts: Record<string, number> = { open: 0, in_progress: 0, on_hold: 0 };
    for (const t of tickets) {
      if (t.status in counts) counts[t.status]++;
    }
    return counts;
  }, [tickets]);

  const toolbar = (
    <div className={`flex gap-2 pb-4 ${isCompact ? "flex-col" : "flex-wrap items-center"}`}>
      <div className="flex flex-wrap items-center gap-2">
        {!isCompact && (
          <SegmentedControl
            key={viewMode}
            className="shrink-0"
            segments={[
              { key: "list", title: "List" },
              { key: "kanban", title: "Board" },
            ]}
            initialSegmentKey={viewMode}
            onSelect={(key) => setViewMode(key as "list" | "kanban")}
          />
        )}
        <Button
          variant={myTicketsActive ? variants.accent : variants.ghost}
          size={buttonSizes.compact}
          onClick={() => setMyTicketsActive((v) => !v)}
        >
          My tickets
        </Button>
        <FilterChipsBar
          filters={chipFilters}
          onChange={handleChipFilterChange}
        />
      </div>
      <div className={`${isCompact ? "" : "ml-auto"}`}>
        <Button
          text="Create ticket"
          variant={variants.secondary}
          size={buttonSizes.compact}
          onClick={() => setShowCreateModal(true)}
        />
      </div>
    </div>
  );

  return (
    <div className="flex flex-col">
      {queueStats.overdue > 0 && !overdueDismissed && (
        <div className="mb-4 flex items-center gap-3 rounded-xl border border-border-5 px-4 py-3">
          <Info width="w-5" className="shrink-0 text-text-primary" />
          <div className="flex flex-1 flex-col">
            <span className="text-emphasis-300 font-medium">{queueStats.overdue} ticket{queueStats.overdue > 1 ? "s" : ""} overdue</span>
            <span className="text-300 text-text-primary-70">These tickets have been open for more than 3 days.</span>
          </div>
          <Button
            text="View"
            variant={variants.secondary}
            size={buttonSizes.compact}
            onClick={() => {
              setActiveDropdownFilters((prev) => ({ ...prev, status: ["open"] }));
              setOverdueDismissed(true);
            }}
          />
          <Button
            ariaLabel="Dismiss"
            variant={variants.ghost}
            size={buttonSizes.compact}
            prefixIcon={<Dismiss />}
            onClick={() => setOverdueDismissed(true)}
          />
        </div>
      )}
      {toolbar}
      {viewMode === "list" ? (
        <List
          items={filteredTickets}
          itemKey="id"
          activeCols={isCompact ? PHONE_COLS : DESKTOP_COLS}
          colTitles={colTitles}
          colConfig={colConfig}
          actions={rowActions}
          itemSelectable
          stickyFirstColumn={false}
          overflowContainer={false}
          total={filteredTickets.length}
          itemName={{ singular: "ticket", plural: "tickets" }}
          sortableColumns={new Set<TicketColumns>(["issue", "asset", "location", "status"])}
          onRowClick={handleRowClick}
          renderActionBar={renderActionBar}
        />
      ) : (
        <TicketKanbanView tickets={filteredTickets} statusCounts={statusCounts} onCardClick={handleRowClick} />
      )}

      {detailTicketId !== null && (
        <TicketDetailModal
          ticketId={detailTicketId}
          ticketIds={tickets.map((t) => t.id)}
          onDismiss={() => setDetailTicketId(null)}
        />
      )}

      {showCreateModal && (
        <CreateTicketModal
          onDismiss={() => setShowCreateModal(false)}
          onSuccess={() => {
            setShowCreateModal(false);
          }}
        />
      )}

      {showBulkCloseModal && (
        <BulkCloseModal
          ticketIds={selectedTicketIds}
          onDismiss={() => setShowBulkCloseModal(false)}
          onSuccess={() => {
            setShowBulkCloseModal(false);
            setSelectedTicketIds([]);
          }}
        />
      )}
    </div>
  );
};

const KANBAN_COLUMNS = [
  { key: "open", label: "Open" },
  { key: "in_progress", label: "In Progress" },
  { key: "on_hold", label: "On Hold" },
] as const;

const TicketKanbanView = ({
  tickets,
  statusCounts,
  onCardClick,
}: {
  tickets: TicketItem[];
  statusCounts: Record<string, number>;
  onCardClick: (ticket: TicketItem) => void;
}) => (
  <div className="grid grid-cols-3 gap-6 max-[1100px]:grid-cols-2 max-[768px]:grid-cols-1">
    {KANBAN_COLUMNS.map((col) => {
      const colTickets = tickets.filter((t) => t.status === col.key);
      return (
        <div key={col.key} className="flex flex-col">
          <div className="pb-3 text-300 text-text-primary-70">
            {col.label} ({statusCounts[col.key] ?? 0})
          </div>
          <div className="flex flex-col gap-2">
            {colTickets.length === 0 ? (
              <div className="flex min-h-32 items-center justify-center text-300 text-text-primary-70">
                No tickets
              </div>
            ) : (
              colTickets.map((ticket) => {
                const metaParts = [
                  ticket.minerIdentifier ?? ticket.buildingName,
                  ticket.assigneeName ?? "Unassigned",
                  ticket.age,
                ].filter(Boolean);

                return (
                  <button
                    key={ticket.id}
                    type="button"
                    className="flex cursor-pointer flex-col rounded-xl bg-surface-5 px-5 py-4 text-left transition-colors hover:bg-surface-10"
                    onClick={() => onCardClick(ticket)}
                  >
                    <div className="flex w-full items-center justify-between pb-3">
                      <span className="text-200 text-text-primary-70">{ticket.ticketNumber}</span>
                      <div className={getComponentIconColor(ticket.urgent)}>
                        {getComponentIcon(ticket.component, ticket.urgent)}
                      </div>
                    </div>
                    <span className="pb-2 text-300 font-medium text-text-primary">
                      {ticket.diagnosis}
                    </span>
                    <span className="text-200 text-text-primary-70">
                      {metaParts.join(", ")}
                    </span>
                  </button>
                );
              })
            )}
          </div>
        </div>
      );
    })}
  </div>
);

export default TicketQueue;
