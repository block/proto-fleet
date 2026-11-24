import { formatReading, latest, temperatureValue } from "../utils/buildContainerHome";
import ContainerSystem from "./ContainerSystem";
import type { FanHardware, PlantTelemetry } from "@/protoOS/store/contract";
import { useFlowUnit, useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import useMinerStore from "@/protoOS/store/useMinerStore";
import Stats from "@/shared/components/Stats";
import { formatFlowRate } from "@/shared/utils/flowRate";

interface Props {
  plant?: PlantTelemetry;
  fans: FanHardware[];
  power: number | null;
}

export default function ContainerSystemView({ plant, fans, power }: Props) {
  const temperatureUnit = useTemperatureUnit();
  const flowUnit = useFlowUnit();
  const fanTelemetry = useMinerStore((state) => state.telemetry.fans);
  const speeds = fans.map((fan) => latest(fanTelemetry.get(fan.ref.id)?.speedPercent));
  const averageFanSpeed =
    speeds.length && speeds.every((speed) => speed !== null)
      ? speeds.reduce((sum, speed) => sum + speed, 0) / speeds.length
      : null;
  const temperature = (value: number | null) =>
    formatReading(temperatureValue(value, temperatureUnit), `°${temperatureUnit}`);
  const pressure = (value: number | null) => formatReading(value === null ? null : value / 6_894.757, "psi");
  const stats = [
    { label: "Container power", value: formatReading(power === null ? null : power / 1000, "kW") },
    { label: "Flow rate", value: formatFlowRate(latest(plant?.flowRate), flowUnit) },
    { label: "Tank inlet temp", value: temperature(latest(plant?.inletTemperature)) },
    { label: "Tank outlet temp", value: temperature(latest(plant?.outletTemperature)) },
    { label: "Dry cooler outlet pressure", value: pressure(latest(plant?.heatRejectionOutletPressure)) },
    { label: "Pump outlet pressure", value: pressure(latest(plant?.pumpOutletPressure)) },
    { label: "Pump speed", value: formatReading(latest(plant?.coolantPumpSpeed), "Hz") },
    { label: "Avg fan speed", value: formatReading(averageFanSpeed, "%") },
    { label: "Internal temp", value: temperature(latest(plant?.insideAmbientTemperature)) },
    { label: "Ambient temp", value: temperature(latest(plant?.outsideAmbientTemperature)) },
  ];

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <h1 className="sr-only">Container system</h1>
      <ContainerSystem plant={plant} fans={fans} />
      <section aria-label="Live readings">
        <Stats
          stats={stats.map((stat) => ({ ...stat, preformatted: true }))}
          grid="grid-cols-2 items-end tablet:grid-cols-3 desktop:grid-cols-5"
          gap="gap-x-4 gap-y-6"
          padding="pb-6"
        />
      </section>
    </div>
  );
}
