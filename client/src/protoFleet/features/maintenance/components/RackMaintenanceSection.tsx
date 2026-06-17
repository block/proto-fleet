import { useMemo, useState } from "react";

import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List, { type ColConfig, type ColTitles } from "@/shared/components/List";
import { Alert } from "@/shared/assets/icons";

type TicketColumns = "urgent" | "issue" | "asset" | "status";

interface TicketItem {
  id: string;
  ticketNumber: string;
  urgent: boolean;
  component: string;
  diagnosis: string;
  minerIdentifier: string | null;
  status: string;
  assigneeName: string | null;
}

const activeCols: TicketColumns[] = ["urgent", "issue", "asset", "status"];

const colTitles: ColTitles<TicketColumns> = {
  urgent: "",
  issue: "Issue",
  asset: "Asset",
  status: "Status",
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

interface RackMaintenanceSectionProps {
  rackId: bigint;
  onTicketClick?: (ticketId: string) => void;
  onCreateTicket?: () => void;
}

const RackMaintenanceSection = ({ rackId, onTicketClick, onCreateTicket }: RackMaintenanceSectionProps) => {
  const [tickets] = useState<TicketItem[]>([]);

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
        width: "w-60",
      },
      asset: {
        component: (ticket) => (
          <span className="text-300">{ticket.minerIdentifier ?? ticket.component}</span>
        ),
        width: "w-36",
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
    }),
    [],
  );

  if (tickets.length === 0) return null;

  return (
    <section className="p-6 laptop:p-10">
      <div className="flex items-center justify-between pb-4">
        <div className="text-heading-200 text-text-primary">
          Maintenance ({tickets.length})
        </div>
        {onCreateTicket && (
          <Button
            text="Create ticket"
            variant={variants.secondary}
            size={buttonSizes.compact}
            onClick={onCreateTicket}
          />
        )}
      </div>
      <List
        items={tickets}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        onRowClick={onTicketClick ? (ticket) => onTicketClick(ticket.id) : undefined}
      />
    </section>
  );
};

export default RackMaintenanceSection;
