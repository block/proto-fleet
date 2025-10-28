import { useState } from "react";
import ControlBoardInfoModal from "./ControlBoardInfoModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import { useControlBoard, useSystemInfo } from "@/protoOS/store";
import LatencyValue from "@/shared/components/LatencyValue";

function ControlBoardStatusCard() {
  // Fetch data directly from store
  const controlBoard = useControlBoard();
  const systemInfo = useSystemInfo();
  const [isModalOpen, setIsModalOpen] = useState(false);

  // Compute display values
  const name = "Control Board";
  const latency = 0; // TODO: Add latency field to ControlBoardInfo type
  const cpuCapacity = systemInfo?.os?.status?.cpu_load_percent || 0;
  // TODO: Add hasWarning logic based on error state

  const formatCpuCapacity = (capacity: number) => {
    return capacity.toFixed(1) + "%";
  };

  return (
    <Card>
      <CardHeader
        title={name}
        statusIcon={null /* TODO: Add warning icon based on error state */}
        onInfoIconClick={() => setIsModalOpen(true)}
      />

      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        <LabeledValue
          value={<LatencyValue value={latency} />}
          label="Latency"
        />
        <LabeledValue
          value={formatCpuCapacity(cpuCapacity)}
          label="CPU capacity"
        />
      </div>
      {isModalOpen && controlBoard && (
        <ControlBoardInfoModal
          controlBoardData={{
            name,
            latency,
            cpuCapacity,
            meta: {
              serialNumber: controlBoard.serial,
              boardId: controlBoard.boardId,
              machineName: controlBoard.machineName,
              firmwareName: controlBoard.firmware?.name,
              firmwareVersion: controlBoard.firmware?.version,
              firmwareVariant: controlBoard.firmware?.variant,
              gitHash: controlBoard.firmware?.gitHash,
              hardware: controlBoard.mpu?.hardware,
              modelName: controlBoard.mpu?.modelName,
            },
          }}
          onDismiss={() => setIsModalOpen(false)}
        />
      )}
    </Card>
  );
}

export default ControlBoardStatusCard;
