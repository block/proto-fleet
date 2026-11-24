import { useState } from "react";
import ContainerStatusModal from "./ContainerStatusModal";
import TankPowerConfirmationDialog from "./TankPowerConfirmationDialog";
import type { Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import ContainerOverviewLayout, {
  type ContainerOverviewLayoutProps,
} from "@/protoFleet/features/containers/components/ContainerOverviewLayout";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import DurationSelector, { type FleetDuration, fleetDurations } from "@/shared/components/DurationSelector";

export interface ContainerOverviewProps extends ContainerOverviewLayoutProps {
  metrics?: Metric[];
}
export default function ContainerOverview(props: ContainerOverviewProps) {
  const [duration, setDuration] = useState<FleetDuration>("24h");
  const [pending, setPending] = useState<{ id: string; label: string; on: boolean } | null>(null);
  const [tankId, setTankId] = useState<string>();
  const [fanId, setFanId] = useState<string>();
  return (
    <>
      <ContainerOverviewLayout
        {...props}
        controls={props.onToggleControl && props.onResetAlarm && props.onMuteAlarm ? props.controls : undefined}
        onToggleTank={(id, on) => setPending({ id, on, label: props.tanks.find((t) => t.id === id)?.label ?? id })}
        onTankInfo={props.onTankInfo ?? setTankId}
        onFanInfo={props.onFanInfo ?? setFanId}
        performance={
          <section className="flex flex-col gap-4">
            <div className="flex items-center justify-between">
              <h2 className="text-heading-200">Performance</h2>
              <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
            </div>
            <DeviceSetPerformanceSection cardSurface="overlay" duration={duration} metrics={props.metrics} />
          </section>
        }
      />
      <TankPowerConfirmationDialog
        open={pending !== null}
        label={pending?.label ?? ""}
        turningOn={pending?.on ?? false}
        onCancel={() => setPending(null)}
        onConfirm={() => {
          if (pending) props.onToggleTank?.(pending.id, pending.on);
          setPending(null);
        }}
      />
      <ContainerStatusModal
        tank={props.tanks.find((t) => t.id === tankId)}
        fan={props.fans.find((f) => f.id === fanId)}
        onClose={() => {
          setTankId(undefined);
          setFanId(undefined);
        }}
      />
    </>
  );
}
