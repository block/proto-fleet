import { useState } from "react";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import { usePsuObservationTime } from "@/protoOS/hooks/usePsuObservationTime";
import { useMinerStore } from "@/protoOS/store";
import type { ComponentId, MetricTelemetry } from "@/protoOS/store/contract";
import { useErrorsByRef } from "@/protoOS/store/hooks/useMinerStatus";
import { getPsuId } from "@/protoOS/store/utils/entityIds";
import { psuFaultReportNotice, psuFreshness, psuHealth, psuReadingNotice } from "@/protoOS/store/utils/psuFreshness";
import { Alert, PsuIndicatorV2 as PsuIndicator } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import PowerValue from "@/shared/components/PowerValue";
import PsuCard from "@/shared/components/PsuCard";
import TemperatureValue from "@/shared/components/TemperatureValue";
import VoltageValue from "@/shared/components/VoltageValue";

type PsuStatusCardProps = ({ slot: number; componentId?: never } | { componentId: ComponentId; slot?: never }) & {
  // Container modules group the single PSU with the controllers under one
  // "Hardware" section, where the slot-position indicator is meaningless clutter.
  // Rigs omit this prop, so the indicator renders exactly as before.
  showComponentIcon?: boolean;
};

function PsuStatusCard({ slot, componentId, showComponentIcon = componentId === undefined }: PsuStatusCardProps) {
  const id = componentId ?? getPsuId(slot!);
  const hardware = useMinerStore((state) => state.hardware.psus.get(id));
  const psuData = useMinerStore((state) => state.telemetry.psus.get(id));
  const now = usePsuObservationTime(componentId !== undefined);
  const freshness = psuFreshness(psuData, now);
  const [showComponentStatusModal, setShowComponentStatusModal] = useState(false);
  const value = (metric: MetricTelemetry | undefined, group: "electrical" | "temperatures") => {
    const reading = metric?.latest?.value;
    return (!componentId || freshness[group] === "current") && reading != null && Number.isFinite(reading)
      ? reading
      : undefined;
  };
  const inputVoltage = value(psuData?.inputVoltage, "electrical");
  const outputVoltage = value(psuData?.outputVoltage, "electrical");
  const inputPower = value(psuData?.inputPower, "electrical");
  const outputPower = value(psuData?.outputPower, "electrical");
  const inputCurrent = value(psuData?.inputCurrent, "electrical");
  const outputCurrent = value(psuData?.outputCurrent, "electrical");
  const avgTemp = value(psuData?.averageTemperature, "temperatures");
  const maxTemp = value(psuData?.hotspotTemperature, "temperatures");
  const position = hardware?.slot ?? slot ?? 1;
  const name = `PSU ${position}`;
  const component = hardware?.ref;
  const errors = useErrorsByRef(component);
  const health = psuHealth(psuData, now);
  const hasErrors = errors.length > 0 || health === "fault" || health === "degraded";
  const unavailable = <span aria-label="Unavailable">—</span>;
  const inputVoltageStat = {
    label: "Input voltage",
    value: inputVoltage === undefined ? unavailable : <VoltageValue value={inputVoltage} />,
  };
  const outputVoltageStat = {
    label: "Output voltage",
    value: outputVoltage === undefined ? unavailable : <VoltageValue value={outputVoltage} />,
  };
  const inputPowerStat = {
    label: "Input power",
    value:
      inputPower === undefined ? (
        unavailable
      ) : componentId ? (
        `${(inputPower / 1000).toFixed(1)} kW`
      ) : (
        <PowerValue value={inputPower} />
      ),
  };
  const outputPowerStat = {
    label: "Output power",
    value:
      outputPower === undefined ? (
        unavailable
      ) : componentId ? (
        `${(outputPower / 1000).toFixed(1)} kW`
      ) : (
        <PowerValue value={outputPower} />
      ),
  };
  const temperatureStats = [
    { label: "Avg temp", value: avgTemp === undefined ? unavailable : <TemperatureValue value={avgTemp} /> },
    { label: "High temp", value: maxTemp === undefined ? unavailable : <TemperatureValue value={maxTemp} /> },
  ];
  const groups = componentId
    ? [
        {
          label: "AC input",
          metrics: [
            { ...inputVoltageStat, label: "Voltage" },
            { label: "Current", value: inputCurrent === undefined ? unavailable : `${inputCurrent.toFixed(1)} A` },
            { ...inputPowerStat, label: "Power" },
          ],
          status: psuReadingNotice("Readings", freshness.electrical),
        },
        {
          label: "DC output",
          metrics: [
            { ...outputVoltageStat, label: "Voltage" },
            { label: "Current", value: outputCurrent === undefined ? unavailable : `${outputCurrent.toFixed(1)} A` },
            { ...outputPowerStat, label: "Power" },
          ],
          status: psuReadingNotice("Readings", freshness.electrical),
        },
        {
          label: "Temperature",
          metrics: temperatureStats,
          status: psuReadingNotice("Readings", freshness.temperatures),
        },
      ]
    : undefined;

  return (
    <>
      <PsuCard
        title={name}
        statusIcon={
          hasErrors ? (
            <span role="img" aria-label={`${name} needs attention`}>
              <Alert className="text-intent-critical-fill" width={iconSizes.small} />
            </span>
          ) : null
        }
        componentIcon={showComponentIcon ? <PsuIndicator position={position} /> : null}
        onInfoClick={component ? () => setShowComponentStatusModal(true) : undefined}
        groups={groups}
        status={
          componentId
            ? [psuReadingNotice("Thermal status", freshness.thermalState), psuFaultReportNotice(errors, now)]
                .filter(Boolean)
                .join(". ")
            : undefined
        }
        metrics={
          componentId
            ? undefined
            : [inputVoltageStat, outputVoltageStat, inputPowerStat, outputPowerStat, ...temperatureStats]
        }
      />
      {showComponentStatusModal && component ? (
        <StatusModal
          open={showComponentStatusModal}
          onClose={() => setShowComponentStatusModal(false)}
          componentAddress={component}
          showBackButton={false}
        />
      ) : null}
    </>
  );
}

export default PsuStatusCard;
