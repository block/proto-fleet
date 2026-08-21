import StatusDot from "@/protoFleet/features/alerts/components/StatusDot";
import type { ValidationState } from "@/protoFleet/features/alerts/types";

interface ChannelStatusBadgeProps {
  state: ValidationState;
}

const LABEL: Record<ValidationState, string> = {
  ok: "Validated",
  failed: "Failed",
  pending: "Not tested",
};

const DOT_CLASS: Record<ValidationState, string> = {
  ok: "bg-intent-success-fill",
  failed: "bg-intent-critical-fill",
  pending: "bg-border-20",
};

const ChannelStatusBadge = ({ state }: ChannelStatusBadgeProps) => (
  <StatusDot dotClass={DOT_CLASS[state]}>{LABEL[state]}</StatusDot>
);

export default ChannelStatusBadge;
