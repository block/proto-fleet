import { useCallback, useMemo, useState } from "react";

import TicketDetailModal from "../TicketDetail/TicketDetailModal";
import CreateTicketModal from "../CreateTicket/CreateTicketModal";
import BulkCloseModal from "../BulkClose/BulkCloseModal";
import { mockTickets, CURRENT_USER } from "../../mockData";
import { Alert } from "@/shared/assets/icons";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List, { type SelectionMode } from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import FilterChipsBar, { type FilterChipsBarFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import SegmentedControl from "@/shared/components/SegmentedControl";

import { Dismiss } from "@/shared/assets/icons";

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

const statusDotColor = (status: string) => {
  switch (status) {
    case "open":
      return "bg-color-text-emphasis";
    case "in_progress":
      return "bg-intent-success-fill";
    case "on_hold":
      return "bg-border-20";
    case "sent_to_vendor":
      return "bg-border-20";
    case "completed":
      return "bg-intent-success-fill";
    default:
      return "bg-border-10";
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

const activeCols: TicketColumns[] = ["urgent", "issue", "asset", "location", "status"];

const colTitles: ColTitles<TicketColumns> = {
  urgent: "",
  issue: "Issue",
  asset: "Asset",
  location: "Location",
  status: "Status",
};

const TicketQueue = () => {
  const [tickets] = useState<TicketItem[]>(mockTickets);
  const [searchQuery, setSearchQuery] = useState("");
  const [viewMode, setViewMode] = useState("list");
  const [detailTicketId, setDetailTicketId] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showBulkCloseModal, setShowBulkCloseModal] = useState(false);
  const [selectedTicketIds, setSelectedTicketIds] = useState<string[]>([]);

  const colConfig: ColConfig<TicketItem, string, TicketColumns> = useMemo(
    () => ({
      urgent: {
        component: (ticket) =>
          ticket.urgent ? (
            <div className="flex items-center justify-center text-text-critical">
              <Alert width="w-4" />
            </div>
          ) : null,
        width: "w-10",
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
        width: "w-72",
      },
      asset: {
        component: (ticket) => (
          <span className="text-300">
            {ticket.minerIdentifier ?? ticket.component}
          </span>
        ),
        width: "w-40",
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
        width: "w-48",
      },
      status: {
        component: (ticket) => (
          <div className="flex flex-col">
            <div className="flex items-center gap-2">
              <div className={`h-2 w-2 flex-shrink-0 rounded-full ${statusDotColor(ticket.status)}`} />
              <span className="text-300">{formatStatus(ticket.status)}</span>
            </div>
            {ticket.assigneeName && (
              <span className="text-300 text-text-primary-70">{ticket.assigneeName}</span>
            )}
          </div>
        ),
        width: "w-40",
      },
    }),
    [],
  );

  const [myTicketsActive, setMyTicketsActive] = useState(false);
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
      if (searchQuery) {
        const q = searchQuery.toLowerCase();
        const haystack = `${ticket.ticketNumber} ${ticket.component} ${ticket.diagnosis} ${ticket.minerIdentifier ?? ""} ${ticket.assigneeName ?? ""} ${ticket.siteName} ${ticket.buildingName}`.toLowerCase();
        if (!haystack.includes(q)) return false;
      }
      return true;
    },
    [searchQuery, myTicketsActive, activeDropdownFilters],
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

  const searchPill = (
    <div className="relative flex items-center">
      <input
        type="text"
        placeholder="Search"
        value={searchQuery}
        onChange={(e) => setSearchQuery(e.target.value)}
        className="h-8 w-44 rounded-full border border-border-5 bg-transparent px-3 text-300 text-text-primary outline-none placeholder:text-text-primary-70 focus:border-border-20"
      />
      {searchQuery && (
        <button
          type="button"
          className="absolute right-2 flex cursor-pointer items-center text-text-primary-70 hover:text-text-primary"
          onClick={() => setSearchQuery("")}
        >
          <Dismiss width="w-3" />
        </button>
      )}
    </div>
  );

  const queueStats = useMemo(() => {
    const active = tickets.filter((t) => t.status !== "completed");
    const open = active.filter((t) => t.status === "open").length;
    const inProgress = active.filter((t) => t.status === "in_progress").length;
    const onHold = active.filter((t) => t.status === "on_hold").length;
    const unassigned = active.filter((t) => !t.assigneeName).length;
    const overdue = active.filter((t) => t.age.includes("d") && parseInt(t.age) >= 3).length;
    const ageHours = active.map((t) => {
      if (t.age.includes("d")) return parseInt(t.age) * 24;
      return parseInt(t.age) || 0;
    });
    const avg = ageHours.length ? Math.round(ageHours.reduce((s, h) => s + h, 0) / ageHours.length) : 0;
    const avgAge = avg >= 24 ? `${Math.round(avg / 24)}d` : `${avg}h`;
    return { open, inProgress, onHold, unassigned, overdue, avgAge, total: active.length };
  }, [tickets]);

  const statusCounts = useMemo(() => {
    const counts: Record<string, number> = { open: 0, in_progress: 0, on_hold: 0 };
    for (const t of tickets) {
      if (t.status in counts) counts[t.status]++;
    }
    return counts;
  }, [tickets]);

  const toolbar = (
    <div className="flex items-center gap-2 pb-4">
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
      <button
        type="button"
        className={`shrink-0 cursor-pointer rounded-full border px-3 py-1 text-300 transition-colors ${
          myTicketsActive ? "border-core-primary-fill bg-core-primary-5 text-text-primary" : "border-border-5 text-text-primary hover:border-border-20"
        }`}
        onClick={() => setMyTicketsActive((v) => !v)}
      >
        My tickets
      </button>
      <FilterChipsBar
        filters={chipFilters}
        onChange={handleChipFilterChange}
      />
      <div className="ml-auto flex shrink-0 items-center gap-2">
        {searchPill}
        <Button
          text="Create ticket"
          variant={variants.primary}
          size={buttonSizes.compact}
          onClick={() => setShowCreateModal(true)}
        />
      </div>
    </div>
  );

  return (
    <div className="flex flex-col">
      <div className="flex pb-4">
        <QueueStat label="Open" value={String(queueStats.open)} />
        <QueueStat label="In progress" value={String(queueStats.inProgress)} />
        <QueueStat label="Unassigned" value={String(queueStats.unassigned)} />
        <QueueStat label="Avg age" value={queueStats.avgAge} />
        <QueueStat label="Overdue" value={String(queueStats.overdue)} critical={queueStats.overdue > 0} />
      </div>
      {toolbar}
      {viewMode === "list" ? (
        <List
          items={filteredTickets}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          actions={rowActions}
          itemSelectable
          stickyFirstColumn={false}
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
                      {ticket.urgent && (
                        <div className="text-text-critical">
                          <Alert width="w-4" />
                        </div>
                      )}
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

const QueueStat = ({ label, value, critical }: { label: string; value: string; critical?: boolean }) => (
  <div className="flex flex-1 flex-col gap-0.5">
    <span className="text-200 text-text-primary-70">{label}</span>
    <span className={`text-heading-200 ${critical ? "text-text-critical" : "text-text-primary"}`}>{value}</span>
  </div>
);

export default TicketQueue;
