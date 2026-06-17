import { useCallback, useMemo, useState } from "react";

import { mockCompletedTickets, REPAIR_TECHNICIANS } from "../../mockData";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import type { DropdownFilterItem, FilterItem } from "@/shared/components/List/Filters/types";

type HistoryColumns = "issue" | "asset" | "resolution" | "completedAt" | "assignee";

interface CompletedTicketItem {
  id: string;
  ticketNumber: string;
  component: string;
  diagnosis: string;
  minerIdentifier: string | null;
  resolution: string;
  assigneeName: string | null;
  completedAt: string;
  siteName: string;
  buildingName: string;
}

const activeCols: HistoryColumns[] = ["issue", "asset", "resolution", "completedAt", "assignee"];

const colTitles: ColTitles<HistoryColumns> = {
  issue: "Issue",
  asset: "Asset",
  resolution: "Resolution",
  completedAt: "Completed",
  assignee: "Technician",
};

const HistoryTab = () => {
  const [tickets] = useState<CompletedTicketItem[]>(mockCompletedTickets);

  const colConfig: ColConfig<CompletedTicketItem, string, HistoryColumns> = useMemo(
    () => ({
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
          <div className="flex flex-col">
            <span className="text-300">{ticket.minerIdentifier ?? ticket.component}</span>
            <span className="text-300 text-text-primary-70">
              {ticket.buildingName}, {ticket.siteName}
            </span>
          </div>
        ),
        width: "w-40",
      },
      resolution: {
        component: (ticket) => <span className="text-300">{ticket.resolution}</span>,
        width: "w-32",
      },
      completedAt: {
        component: (ticket) => <span className="text-300">{ticket.completedAt}</span>,
        width: "w-36",
      },
      assignee: {
        component: (ticket) => (
          <span className="text-300">{ticket.assigneeName ?? "Unassigned"}</span>
        ),
        width: "w-32",
      },
    }),
    [],
  );

  const filters: FilterItem[] = useMemo(
    () => [
      {
        type: "dropdown",
        title: "Component",
        value: "component",
        options: [
          { id: "fan", label: "Fan" },
          { id: "hashboard", label: "Hashboard" },
          { id: "psu", label: "PSU" },
          { id: "control_board", label: "Control Board" },
          { id: "network", label: "Network" },
          { id: "electrical", label: "Electrical" },
          { id: "hvac", label: "HVAC" },
          { id: "cleaning", label: "Cleaning" },
          { id: "building", label: "Building" },
        ],
        defaultOptionIds: [],
      } satisfies DropdownFilterItem,
      {
        type: "dropdown",
        title: "Technician",
        value: "technician",
        options: REPAIR_TECHNICIANS.map((t) => ({ id: t, label: t })),
        defaultOptionIds: [],
      } satisfies DropdownFilterItem,
    ],
    [],
  );

  const handleExport = useCallback(() => {
    // TODO: wire to ExportTicketsCsv RPC
  }, []);

  return (
    <div className="flex flex-col gap-4">
      <List
        items={tickets}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        filters={filters}
        stickyFirstColumn={false}
        total={tickets.length}
        itemName={{ singular: "completed ticket", plural: "completed tickets" }}
        sortableColumns={new Set<HistoryColumns>(["issue", "asset", "resolution"])}
        headerControls={
          <Button
            text="Export CSV"
            variant={variants.secondary}
            size={buttonSizes.compact}
            onClick={handleExport}
          />
        }
      />
    </div>
  );
};

export default HistoryTab;
