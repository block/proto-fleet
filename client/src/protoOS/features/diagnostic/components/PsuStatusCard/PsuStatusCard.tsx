import { useState } from "react";
import PsuInfoModal from "./PsuInfoModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import type { PsuData } from "@/protoOS/features/diagnostic/types";
import { Alert, PsuIndicatorV2 as PsuIndicator } from "@/shared/assets/icons";
import PowerValue from "@/shared/components/PowerValue";
import TemperatureValue from "@/shared/components/TemperatureValue";
import VoltageValue from "@/shared/components/VoltageValue";

export type { PsuData };

interface PsuStatusCardProps {
  psuData: PsuData;
}

function PsuStatusCard({ psuData }: PsuStatusCardProps) {
  const [isModalOpen, setIsModalOpen] = useState(false);

  return (
    <Card>
      <CardHeader
        title={psuData.name}
        statusIcon={
          psuData.hasWarning ? <Alert className="text-text-critical" /> : null
        }
        componentIcon={<PsuIndicator position={psuData.position} />}
        onInfoIconClick={() => setIsModalOpen(true)}
      />

      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        <LabeledValue
          value={<VoltageValue value={psuData.inputVoltage} />}
          label="Input voltage"
        />
        <LabeledValue
          value={<VoltageValue value={psuData.outputVoltage} />}
          label="Output voltage"
        />
        <LabeledValue
          value={<PowerValue value={psuData.inputPower} />}
          label="Input power"
        />
        <LabeledValue
          value={<PowerValue value={psuData.outputPower} />}
          label="Output power"
        />
        <LabeledValue
          value={<TemperatureValue value={psuData.avgTemp} />}
          label="Avg temp"
        />
        <LabeledValue
          value={<TemperatureValue value={psuData.maxTemp} />}
          label="High temp"
        />
      </div>
      {isModalOpen && (
        <PsuInfoModal
          psuData={psuData}
          onDismiss={() => setIsModalOpen(false)}
        />
      )}
    </Card>
  );
}

export default PsuStatusCard;
