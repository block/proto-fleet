import { useState } from "react";
import { ProtoOSStatusModal } from "@/protoOS/components/StatusModal";
import { useMinerStatusCircle, useMinerStatusSummary } from "@/protoOS/hooks/status";
import StatusPill from "@/shared/components/StatusPill";

const MinerStatus = () => {
  // Get widget display data
  const summary = useMinerStatusSummary();
  const circle = useMinerStatusCircle();

  // Local state for modal visibility
  const [isModalOpen, setModalOpen] = useState(false);

  return (
    <div className="relative">
      <StatusPill onClick={() => setModalOpen(true)} summary={summary} circle={circle} />

      {/* ProtoOS StatusModal handles both WakingDialog and StatusModal internally */}

      <ProtoOSStatusModal open={isModalOpen} onClose={() => setModalOpen(false)} />
    </div>
  );
};

export default MinerStatus;
