import { useState } from "react";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import { useErrorsByComponent, useMinerPsuById } from "@/protoOS/store";
import type { ComponentId } from "@/protoOS/store/contract";
import { getPsuId } from "@/protoOS/store/utils/entityIds";
import { Alert, PsuIndicatorV2 as PsuIndicator } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import PowerValue from "@/shared/components/PowerValue";
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

  const errors = useErrorsByComponent("PSU", slot ?? 1);
  const hasErrors = componentId ? psuData?.health === "fault" || psuData?.health === "degraded" : errors.length > 0;

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
    <Card>
      <CardHeader
        title={name}
        statusIcon={
          hasErrors ? (
            <span role="img" aria-label={`${name} needs attention`}>
              <Alert className="text-intent-critical-fill" width={iconSizes.small} />
            </span>
          ) : null
        }
        componentIcon={showComponentIcon ? <PsuIndicator position={position} /> : null}
        onInfoIconClick={
          // TODO(btc-mining/proto-fleet-internal#93): Restore CT1 info when status details resolve canonical component IDs.
          componentId ? undefined : () => setShowComponentStatusModal(true)
        }
      />

      <div className="grid grid-cols-2 gap-x-4 gap-y-3 @3xl:grid-cols-6">
        {electricalStats.map((stat) => (
          <LabeledValue key={stat.label} {...stat} />
        ))}
        <LabeledValue value={avgTemp === undefined ? "—" : <TemperatureValue value={avgTemp} />} label="Avg temp" />
        <LabeledValue value={maxTemp === undefined ? "—" : <TemperatureValue value={maxTemp} />} label="High temp" />
      </div>
      {!componentId ? (
        <StatusModal
          open={showComponentStatusModal}
          onClose={() => setShowComponentStatusModal(false)}
          componentAddress={{
            source: "PSU",
            slot: slot ?? 1,
          }}
          showBackButton={false}
        />
      ) : null}
    </Card>
  );
}

export default PsuStatusCard;
