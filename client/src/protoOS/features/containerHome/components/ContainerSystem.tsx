import { useState } from "react";
import { formatReading, latest, temperatureValue } from "../utils/buildContainerHome";
import ContainerSystemControlsModal from "./ContainerSystemControlsModal";
import ContainerSystemSchematic, { type ContainerSystemSchematicProps } from "./ContainerSystemSchematic";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { tankSide } from "@/protoOS/features/tanks/tankSide";
import type { FanHardware, PlantTelemetry } from "@/protoOS/store/contract";
import { useFlowUnit, useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import useMinerStore from "@/protoOS/store/useMinerStore";
import Button from "@/shared/components/Button";
import { formatFlowRate } from "@/shared/utils/flowRate";

export default function ContainerSystem({ plant, fans }: { plant?: PlantTelemetry; fans: FanHardware[] }) {
  const { adapter } = useMinerHosting();
  const hardware = useMinerStore((state) => state.hardware);
  const telemetry = useMinerStore((state) => state.telemetry);
  const generation = useMinerStore((state) => state.connectionGeneration);
  const temperatureUnit = useTemperatureUnit();
  const flowUnit = useFlowUnit();
  const [selectedGeneration, setSelectedGeneration] = useState<number>();
  const topology = hardware.topology;
  const tanks = topology?.kind === "container" ? [...topology.tanks.values()] : [];
  const temperature = (value: number | null) =>
    formatReading(temperatureValue(value, temperatureUnit), `°${temperatureUnit}`);
  const pressure = (value: number | null) => formatReading(value === null ? null : value / 6_894.757, "psi");
  const bank = (side: "Left" | "Right") => ({
    id: `${side} tanks`,
    tanks: tanks
      .filter((tank) => tankSide(tank.slot) === side)
      .sort((a, b) => a.slot! - b.slot!)
      .map((tank) => ({
        id: tank.id,
        label: String(tank.slot),
        // MDKv2 reports two fluid-volume levels in mm, not six calibrated fill percentages.
        levelPercent: null,
      })),
    temperature: temperature(latest(side === "Left" ? plant?.outletATemperature : plant?.outletBTemperature)),
    fluidLevel: formatReading(latest(side === "Left" ? plant?.tankALiquidLevel : plant?.tankBLiquidLevel), "mm"),
  });
  const props: ContainerSystemSchematicProps = {
    tankBanks: [bank("Left"), bank("Right")],
    fans: fans.map((fan) => {
      const reading = telemetry.fans.get(fan.ref.id);
      return {
        id: fan.ref.id,
        label: fan.name?.match(/\d+$/)?.[0] ?? fan.name ?? "Fan",
        speedPercent: latest(reading?.speedPercent),
        status: reading?.health === "fault" ? "critical" : "normal",
      };
    }),
    flowRate: formatFlowRate(latest(plant?.flowRate), flowUnit),
    pumpSpeed: formatReading(latest(plant?.coolantPumpSpeed), "Hz"),
    dryCoolerInletTemperature: temperature(latest(plant?.dryCoolerInletTemperature)),
    dryCoolerOutletPressure: pressure(latest(plant?.heatRejectionOutletPressure)),
    pumpOutletPressure: pressure(latest(plant?.pumpOutletPressure)),
    actions:
      adapter?.getContainerSettings &&
      (adapter.setContainerControl || adapter.setContainerSetpoint || adapter.setMainCircuitBreakerState) ? (
        <Button
          variant="secondary"
          size="compact"
          text="System controls"
          className="min-h-10"
          onClick={() => setSelectedGeneration(generation)}
        />
      ) : undefined,
  };
  return (
    <>
      <ContainerSystemSchematic {...props} />
      {selectedGeneration === generation ? (
        <ContainerSystemControlsModal key={generation} onDismiss={() => setSelectedGeneration(undefined)} />
      ) : null}
    </>
  );
}
