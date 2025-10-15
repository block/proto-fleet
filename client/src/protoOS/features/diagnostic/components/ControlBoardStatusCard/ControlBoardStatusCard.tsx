import { useState } from "react";
import ControlBoardInfoModal from "./ControlBoardInfoModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import type { ControlBoardData } from "@/protoOS/features/diagnostic/types";
import { Alert } from "@/shared/assets/icons";

export type { ControlBoardData };

interface ControlBoardStatusCardProps {
  controlBoardData: ControlBoardData;
}

function ControlBoardStatusCard({
  controlBoardData,
}: ControlBoardStatusCardProps) {
  const [isModalOpen, setIsModalOpen] = useState(false);
  const formatLatency = (latency: number) => {
    return latency.toFixed(1) + "ms";
  };

  const formatCpuCapacity = (capacity: number) => {
    return capacity.toFixed(1) + "%";
  };

  return (
    <Card>
      <CardHeader
        title={controlBoardData.name}
        statusIcon={
          controlBoardData.hasWarning ? (
            <Alert className="text-text-critical" />
          ) : null
        }
        onInfoIconClick={() => setIsModalOpen(true)}
      />

      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        <LabeledValue
          value={formatLatency(controlBoardData.latency)}
          label="Latency"
        />
        <LabeledValue
          value={formatCpuCapacity(controlBoardData.cpuCapacity)}
          label="CPU capacity"
        />
      </div>
      {isModalOpen && (
        <ControlBoardInfoModal
          onDismiss={() => setIsModalOpen(false)}
          controlBoardData={controlBoardData}
        />
      )}
    </Card>
  );
}

export default ControlBoardStatusCard;
