import { useState } from "react";
import { ProtoOSStatusModal as StatusModal } from "@/protoOS/components/StatusModal";
import Card from "@/protoOS/features/diagnostic/components/Card";
import CardHeader from "@/protoOS/features/diagnostic/components/CardHeader";
import LabeledValue from "@/protoOS/features/diagnostic/components/LabeledValue";
import { useErrorsByComponent, useSystemInfo } from "@/protoOS/store";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import LatencyValue from "@/shared/components/LatencyValue";

interface ControlBoardStatusCardProps {
  // Container modules expose multiple controllers, each with its own
  // title, latency reading, CPU load, and warning state. Rigs call this with no
  // props: the title stays "Control Board", latency is omitted, CPU capacity is
  // read from the store, and warnings come from the error store — byte-identical
  // to the original behavior.
  title?: string;
  latency?: number;
  cpuCapacity?: number;
  hasWarning?: boolean;
}

function ControlBoardStatusCard({ title, latency, cpuCapacity, hasWarning }: ControlBoardStatusCardProps) {
  // Fetch data directly from store
  const systemInfo = useSystemInfo();
  const [showComponentStatusModal, setShowComponentStatusModal] = useState(false);

  // Compute display values
  const name = title ?? "Control Board";
  const cpuLoad = cpuCapacity ?? systemInfo?.os?.status?.cpu_load_percent ?? 0;
  const showLatency = latency !== undefined;

  // Check for errors
  const errors = useErrorsByComponent("RIG", 0);
  const hasErrors = hasWarning || errors.length > 0;

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
        {showLatency ? <LabeledValue value={<LatencyValue value={latency} />} label="Latency" /> : null}
        <LabeledValue value={formatCpuCapacity(cpuLoad)} label="CPU capacity" />
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
