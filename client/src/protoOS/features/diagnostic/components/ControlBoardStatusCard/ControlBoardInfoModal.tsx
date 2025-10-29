import LabeledValue from "../LabeledValue";
import MetadataRow from "../MetadataRow";
import type { ControlBoardData } from "@/protoOS/features/diagnostic/types";
import { ControlBoard } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import LatencyValue from "@/shared/components/LatencyValue";
import Modal from "@/shared/components/Modal";

interface ControlBoardInfoModalProps {
  onDismiss: () => void;
  controlBoardData: ControlBoardData;
}

function ControlBoardInfoModal({
  onDismiss,
  controlBoardData,
}: ControlBoardInfoModalProps) {
  const formatCpuCapacity = (capacity: number) => {
    return capacity.toFixed(1) + "%";
  };

  return (
    <Modal
      onDismiss={onDismiss}
      title="Control board status"
      size="large"
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: onDismiss,
        },
      ]}
    >
      <div className="flex flex-col gap-y-6 py-6">
        <Header
          icon={
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-core-primary-5">
              <ControlBoard />
            </div>
          }
          title={controlBoardData.name}
          titleSize="text-heading-300"
        />

        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          <LabeledValue
            value={<LatencyValue value={controlBoardData.latency} />}
            label="Latency"
            variant="large"
          />
          <LabeledValue
            value={formatCpuCapacity(controlBoardData.cpuCapacity)}
            label="CPU capacity"
            variant="large"
          />
        </div>

        <div className="flex flex-col">
          {controlBoardData.meta.serialNumber && (
            <MetadataRow
              label="Serial number"
              value={controlBoardData.meta.serialNumber}
            />
          )}
          {controlBoardData.meta.boardId && (
            <MetadataRow
              label="Board ID"
              value={controlBoardData.meta.boardId}
            />
          )}
          {controlBoardData.meta.modelName && (
            <MetadataRow
              label="Model name"
              value={controlBoardData.meta.modelName}
            />
          )}
          {controlBoardData.meta.machineName && (
            <MetadataRow
              label="Machine name"
              value={controlBoardData.meta.machineName}
            />
          )}
          {controlBoardData.meta.firmwareName && (
            <MetadataRow
              label="Firmware name"
              value={controlBoardData.meta.firmwareName}
            />
          )}
          {controlBoardData.meta.firmwareVersion && (
            <MetadataRow
              label="Firmware version"
              value={controlBoardData.meta.firmwareVersion}
            />
          )}
          {controlBoardData.meta.firmwareVariant && (
            <MetadataRow
              label="Firmware variant"
              value={controlBoardData.meta.firmwareVariant}
            />
          )}
          {controlBoardData.meta.hardware && (
            <MetadataRow
              label="Hardware"
              value={controlBoardData.meta.hardware}
            />
          )}
        </div>
      </div>
    </Modal>
  );
}

export default ControlBoardInfoModal;
