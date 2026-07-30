import type { Channel, RoutingMode } from "@/protoFleet/features/alerts/types";
import Checkbox from "@/shared/components/Checkbox";
import SegmentedControl from "@/shared/components/SegmentedControl";

const MODE_SEGMENTS: { key: RoutingMode; title: string }[] = [
  { key: "default", title: "All channels" },
  { key: "custom", title: "Selected channels" },
  { key: "none", title: "In-app only" },
];

interface DeliveryPickerProps {
  mode: RoutingMode;
  onModeChange: (mode: RoutingMode) => void;
  channels: Channel[];
  channelsLoaded: boolean;
  selectedIds: Set<string>;
  onToggleChannel: (id: string) => void;
}

// Shared delivery-routing picker: default = every org channel, custom = a picked subset, none = in-app history only.
// Hosts must key this component per editing session (useDeliveryRouting's sessionKey): the SegmentedControl is uncontrolled.
const DeliveryPicker = ({
  mode,
  onModeChange,
  channels,
  channelsLoaded,
  selectedIds,
  onToggleChannel,
}: DeliveryPickerProps) => (
  <div className="flex flex-col gap-4">
    <SegmentedControl
      segments={MODE_SEGMENTS}
      initialSegmentKey={mode}
      onSelect={(key) => onModeChange(key as RoutingMode)}
    />

    {mode === "custom" ? (
      <div className="flex flex-col gap-2">
        {channels.map((channel) => (
          <label
            key={channel.id}
            className="flex cursor-pointer items-center gap-3 rounded-lg border border-border-5 p-3"
          >
            <Checkbox checked={selectedIds.has(channel.id)} onChange={() => onToggleChannel(channel.id)} />
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-text-primary">{channel.name}</span>
              <span className="truncate text-200 text-text-primary-70">
                {channel.kind === "slack" ? "Slack" : "Webhook"}
              </span>
            </span>
          </label>
        ))}
        {channelsLoaded && channels.length === 0 ? (
          <p className="py-4 text-center text-text-primary-50">
            No channels yet — add one in the Channels section first.
          </p>
        ) : null}
      </div>
    ) : (
      <p className="text-200 text-text-primary-50">
        {mode === "default"
          ? "Alerts from this rule are delivered to every channel, including ones added later."
          : "Alerts from this rule show up in the in-app history only; no channel is notified."}
      </p>
    )}
  </div>
);

export default DeliveryPicker;
