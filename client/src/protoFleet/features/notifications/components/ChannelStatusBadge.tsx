import clsx from "clsx";
import type { ValidationState } from "@/protoFleet/features/notifications/types";

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
  <span className="inline-flex items-center gap-2 text-300 text-text-primary-50">
    <span className={clsx("h-2 w-2 rounded-full", DOT_CLASS[state])} />
    {LABEL[state]}
  </span>
);

export default ChannelStatusBadge;
