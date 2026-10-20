import { useState } from "react";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import { useMinerPsuById, useMinerStore } from "@/protoOS/store";
import type { ComponentId } from "@/protoOS/store/contract";
import { useErrorsByRef } from "@/protoOS/store/hooks/useMinerStatus";
import { getPsuId } from "@/protoOS/store/utils/entityIds";
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
  // Fetch data directly from store
  const psuData = useMinerPsuById(componentId ?? getPsuId(slot!));
  const [showComponentStatusModal, setShowComponentStatusModal] = useState(false);

  // Compute display values
  const inputVoltage = psuData?.inputVoltage?.latest?.value ?? undefined;
  const outputVoltage = psuData?.outputVoltage?.latest?.value ?? undefined;
  const inputPower = psuData?.inputPower?.latest?.value ?? undefined;
  const outputPower = psuData?.outputPower?.latest?.value ?? undefined;
  const position = psuData?.slot ?? slot ?? 1;
  const name = `PSU ${position}`;

  const avgTemp = psuData?.averageTemperature?.latest?.value ?? undefined;
  const maxTemp = psuData?.hotspotTemperature?.latest?.value ?? undefined;

  const component = useMinerStore((state) => state.hardware.psus.get(componentId ?? getPsuId(slot!))?.ref);
  const errors = useErrorsByRef(component);
  const hasErrors = errors.length > 0 || psuData?.health === "fault" || psuData?.health === "degraded";

  const inputVoltageStat = {
    label: "Input voltage",
    value: inputVoltage === undefined ? "—" : <VoltageValue value={inputVoltage} />,
  };
  const outputVoltageStat = {
    label: "Output voltage",
    value: outputVoltage === undefined ? "—" : <VoltageValue value={outputVoltage} />,
  };
  const inputPowerStat = {
    label: "Input power",
    value:
      inputPower === undefined ? (
        "—"
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
        "—"
      ) : componentId ? (
        `${(outputPower / 1000).toFixed(1)} kW`
      ) : (
        <PowerValue value={outputPower} />
      ),
  };
  const electricalStats = componentId
    ? [inputVoltageStat, inputPowerStat, outputVoltageStat, outputPowerStat]
    : [inputVoltageStat, outputVoltageStat, inputPowerStat, outputPowerStat];

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
        metrics={[
          ...electricalStats,
          { label: "Avg temp", value: avgTemp === undefined ? "—" : <TemperatureValue value={avgTemp} /> },
          { label: "High temp", value: maxTemp === undefined ? "—" : <TemperatureValue value={maxTemp} /> },
        ]}
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
