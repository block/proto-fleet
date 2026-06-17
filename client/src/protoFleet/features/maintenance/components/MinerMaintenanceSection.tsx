import { useMemo, useState } from "react";

import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Card from "@/shared/components/Card";
import List, { type ColConfig, type ColTitles } from "@/shared/components/List";
import { Alert } from "@/shared/assets/icons";

type TicketColumns = "urgent" | "issue" | "status" | "age";

interface MinerTicketItem {
  id: string;
  ticketNumber: string;
  urgent: boolean;
  component: string;
  diagnosis: string;
  status: string;
  assigneeName: string | null;
  age: string;
}

interface RepairHistoryItem {
  date: string;
  resolution: string;
  partsUsed: string;
  technician: string;
}

const activeCols: TicketColumns[] = ["urgent", "issue", "status", "age"];

const colTitles: ColTitles<TicketColumns> = {
  urgent: "",
  issue: "Issue",
  status: "Status",
  age: "Age",
};

const statusDotColor = (status: string) => {
  switch (status) {
    case "open":
      return "bg-color-text-emphasis";
    case "in_progress":
      return "bg-intent-success-fill";
    case "on_hold":
      return "bg-border-20";
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
    default:
      return status;
  }
};

interface MinerMaintenanceSectionProps {
  minerIdentifier: string;
  onTicketClick?: (ticketId: string) => void;
  onCreateTicket?: () => void;
}

const MinerMaintenanceSection = ({ minerIdentifier, onTicketClick, onCreateTicket }: MinerMaintenanceSectionProps) => {
  const [openTickets] = useState<MinerTicketItem[]>([]);
  const [repairHistory] = useState<RepairHistoryItem[]>([]);

  const colConfig: ColConfig<MinerTicketItem, string, TicketColumns> = useMemo(
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
        width: "w-60",
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
        width: "w-36",
      },
      age: {
        component: (ticket) => <span className="text-300">{ticket.age}</span>,
        width: "w-24",
      },
    }),
    [],
  );

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div className="text-heading-200 text-text-primary">Maintenance</div>
        {onCreateTicket && (
          <Button
            text="Create ticket"
            variant={variants.secondary}
            size={buttonSizes.compact}
            onClick={onCreateTicket}
          />
        )}
      </div>

      {openTickets.length > 0 ? (
        <Card title={`Open tickets (${openTickets.length})`} type="default">
          <List
            items={openTickets}
            itemKey="id"
            activeCols={activeCols}
            colTitles={colTitles}
            colConfig={colConfig}
            onRowClick={onTicketClick ? (ticket) => onTicketClick(ticket.id) : undefined}
          />
        </Card>
      ) : (
        <div className="text-300 text-text-primary-70">No open tickets for this miner.</div>
      )}

      {repairHistory.length > 0 && (
        <div className="flex flex-col gap-3">
          <span className="text-emphasis-300 font-medium">Repair History</span>
          {repairHistory.map((entry, i) => (
            <div key={i} className="flex gap-3 border-l-2 border-border-5 pl-3">
              <div
                className={`mt-1.5 h-2.5 w-2.5 flex-shrink-0 rounded-full ${
                  i === 0 ? "bg-intent-success-fill" : "bg-border-5"
                }`}
              />
              <div className="flex flex-col gap-0.5">
                <span className="text-300 font-medium">{entry.date}</span>
                <span className="text-300">{entry.resolution}</span>
                {entry.partsUsed && (
                  <span className="text-300 text-text-primary-70">Parts: {entry.partsUsed}</span>
                )}
                <span className="text-300 text-text-primary-70">{entry.technician}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default MinerMaintenanceSection;
