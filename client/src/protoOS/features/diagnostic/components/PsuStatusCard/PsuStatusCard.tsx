import { useMemo, useState } from "react";
import PsuInfoModal from "./PsuInfoModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import { useMinerPsu } from "@/protoOS/store";
import { PsuIndicatorV2 as PsuIndicator } from "@/shared/assets/icons";
import PowerValue from "@/shared/components/PowerValue";
import TemperatureValue from "@/shared/components/TemperatureValue";
import VoltageValue from "@/shared/components/VoltageValue";

interface PsuStatusCardProps {
  psuId: number;
}

function PsuStatusCard({ psuId }: PsuStatusCardProps) {
  // Fetch data directly from store
  const psuData = useMinerPsu(psuId);
  const [isModalOpen, setIsModalOpen] = useState(false);

  // Compute display values
  const inputVoltage = psuData?.inputVoltage?.latest?.value ?? 0;
  const outputVoltage = psuData?.outputVoltage?.latest?.value ?? 0;
  const inputPower = psuData?.inputPower?.latest?.value ?? 0;
  const outputPower = psuData?.outputPower?.latest?.value ?? 0;
  const position = psuData?.slot ?? psuId;
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

  // TODO: Add hasWarning logic based on error state

  return (
    <Card>
      <CardHeader
        title={name}
        statusIcon={null /* TODO: Add warning icon based on error state */}
        componentIcon={<PsuIndicator position={position} />}
        onInfoIconClick={() => setIsModalOpen(true)}
      />

      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        <LabeledValue
          value={<VoltageValue value={inputVoltage} />}
          label="Input voltage"
        />
        <LabeledValue
          value={<VoltageValue value={outputVoltage} />}
          label="Output voltage"
        />
        <LabeledValue
          value={<PowerValue value={inputPower} />}
          label="Input power"
        />
        <LabeledValue
          value={<PowerValue value={outputPower} />}
          label="Output power"
        />
        <LabeledValue
          value={<TemperatureValue value={avgTemp} />}
          label="Avg temp"
        />
        <LabeledValue
          value={<TemperatureValue value={maxTemp} />}
          label="High temp"
        />
      </div>
      {isModalOpen && psuData && (
        <PsuInfoModal
          psuData={{
            id: psuId,
            name,
            position,
            inputVoltage,
            outputVoltage,
            inputPower,
            outputPower,
            avgTemp,
            maxTemp,
            meta: {
              serialNumber: psuData.serial,
              manufacturer: psuData.manufacturer,
              model: psuData.model,
              hardwareRevision: psuData.hwRevision,
              firmwareAppVersion: psuData.firmware?.appVersion,
              firmwareBootloaderVersion: psuData.firmware?.bootloaderVersion,
            },
          }}
          onDismiss={() => setIsModalOpen(false)}
        />
      )}
    </Card>
  );
}

export default PsuStatusCard;
