import { useState } from "react";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import { useErrorsByComponent, useSystemInfo } from "@/protoOS/store";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

function ControlBoardStatusCard() {
  // Fetch data directly from store
  const systemInfo = useSystemInfo();
  const [showComponentStatusModal, setShowComponentStatusModal] = useState(false);

  // Compute display values
  const name = "Control Board";
  const cpuCapacity = systemInfo?.os?.status?.cpu_load_percent || 0;

  // Check for errors
  const errors = useErrorsByComponent("RIG", 0);
  const hasErrors = errors.length > 0;

  const formatCpuCapacity = (capacity: number) => {
    return capacity.toFixed(1) + "%";
  };

  return (
    <Card>
      <CardHeader
        title={name}
        statusIcon={hasErrors ? <Alert className="text-intent-critical-fill" width={iconSizes.small} /> : null}
        onInfoIconClick={() => setShowComponentStatusModal(true)}
      />

      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        <LabeledValue value={formatCpuCapacity(cpuCapacity)} label="CPU capacity" />
      </div>
      <StatusModal
        open={showComponentStatusModal}
        onClose={() => setShowComponentStatusModal(false)}
        componentAddress={{
          source: "RIG",
        }}
        showBackButton={false}
      />
    </Card>
  );
}

export default ControlBoardStatusCard;
