import { useMemo, useState } from "react";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import { useErrorsByComponent, useMinerPsu } from "@/protoOS/store";
import { Alert, PsuIndicatorV2 as PsuIndicator } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import PowerValue from "@/shared/components/PowerValue";
import TemperatureValue from "@/shared/components/TemperatureValue";
import VoltageValue from "@/shared/components/VoltageValue";

interface PsuStatusCardProps {
  slot: number;
  // Container modules group the single PSU with the controllers under one
  // "Hardware" section, where the slot-position indicator is meaningless clutter.
  // Rigs omit this prop, so the indicator renders exactly as before.
  showComponentIcon?: boolean;
}

function PsuStatusCard({ slot, showComponentIcon = true }: PsuStatusCardProps) {
  // Fetch data directly from store
  const psuData = useMinerPsu(slot);
  const [showComponentStatusModal, setShowComponentStatusModal] = useState(false);

  // Compute display values
  const inputVoltage = psuData?.inputVoltage?.latest?.value ?? 0;
  const outputVoltage = psuData?.outputVoltage?.latest?.value ?? 0;
  const inputPower = psuData?.inputPower?.latest?.value ?? 0;
  const outputPower = psuData?.outputPower?.latest?.value ?? 0;
  const position = psuData?.slot ?? slot;
  const name = `PSU ${position}`;

  // Calculate avg and max temps from temperature array
  const { avgTemp, maxTemp } = useMemo(() => {
    const averageTemp = psuData?.temperatureAverage?.latest?.value;
    const hotspotTemp = psuData?.temperatureHotspot?.latest?.value;

    return {
      avgTemp: averageTemp, // Use the average temperature directly from API
      maxTemp: hotspotTemp, // Use the hotspot temperature as max
    };
  }, [psuData?.temperatureAverage, psuData?.temperatureHotspot]);

  const errors = useErrorsByComponent("PSU", slot);
  const hasErrors = errors.length > 0;

  return (
    <Card>
      <CardHeader
        title={name}
        statusIcon={hasErrors ? <Alert className="text-intent-critical-fill" width={iconSizes.small} /> : null}
        componentIcon={showComponentIcon ? <PsuIndicator position={position} /> : null}
        onInfoIconClick={() => setShowComponentStatusModal(true)}
      />

      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        <LabeledValue value={<VoltageValue value={inputVoltage} />} label="Input voltage" />
        <LabeledValue value={<VoltageValue value={outputVoltage} />} label="Output voltage" />
        <LabeledValue value={<PowerValue value={inputPower} />} label="Input power" />
        <LabeledValue value={<PowerValue value={outputPower} />} label="Output power" />
        <LabeledValue value={<TemperatureValue value={avgTemp} />} label="Avg temp" />
        <LabeledValue value={<TemperatureValue value={maxTemp} />} label="High temp" />
      </div>
      <StatusModal
        open={showComponentStatusModal}
        onClose={() => setShowComponentStatusModal(false)}
        componentAddress={{
          source: "PSU",
          slot: slot,
        }}
        showBackButton={false}
      />
    </Card>
  );
}

export default PsuStatusCard;
