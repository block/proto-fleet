import { Alert, Checkmark, Info, Pause } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";

interface TicketStatusBannerProps {
  status: string;
  assigneeName: string | null;
  onAssign: () => void;
  onComplete: () => void;
  onResume: () => void;
  onMarkReceived: () => void;
}

const TicketStatusBanner = ({ status, assigneeName, onAssign, onComplete, onResume, onMarkReceived }: TicketStatusBannerProps) => {
  switch (status) {
    case "open":
      return (
        <Callout
          intent="information"
          prefixIcon={<Info width="w-4" />}
          title={assigneeName ? `Assigned to ${assigneeName}` : "Awaiting assignment"}
          subtitle={assigneeName ? "Ready to start" : "No technician assigned"}
          buttonText={assigneeName ? "Start repair" : "Assign"}
          buttonOnClick={assigneeName ? onComplete : onAssign}
        />
      );
    case "in_progress":
      return (
        <Callout
          intent="information"
          prefixIcon={<Info width="w-4" />}
          title="Repair underway"
          subtitle={assigneeName ? `Assigned to ${assigneeName}` : undefined}
          buttonText="Complete repair"
          buttonOnClick={onComplete}
        />
      );
    case "on_hold":
      return (
        <Callout
          intent="warning"
          prefixIcon={<Pause width="w-4" />}
          title="Waiting for parts or info"
          subtitle={assigneeName ? `Assigned to ${assigneeName}` : undefined}
          buttonText="Resume"
          buttonOnClick={onResume}
        />
      );
    case "sent_to_vendor":
      return (
        <Callout
          intent="information"
          prefixIcon={<Alert width="w-4" />}
          title="Awaiting vendor return"
          subtitle={assigneeName ? `Assigned to ${assigneeName}` : undefined}
          buttonText="Mark received"
          buttonOnClick={onMarkReceived}
        />
      );
    case "completed":
      return (
        <Callout
          intent="success"
          prefixIcon={<Checkmark width="w-4" />}
          title="Repair completed"
        />
      );
    default:
      return null;
  }
};

export default TicketStatusBanner;
