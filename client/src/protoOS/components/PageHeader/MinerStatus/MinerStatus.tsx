import { useState } from "react";
import MinerStatusWidget from "./MinerStatusWidget";
import { ProtoOSStatusModal } from "@/protoOS/components/StatusModal";
import { useMinerStatusCircle, useMinerStatusSummary } from "@/protoOS/hooks/status";

const MinerStatus = () => {
  // Get widget display data
  const summary = useMinerStatusSummary();
  const circle = useMinerStatusCircle();

  // Local state for modal visibility
  const [isModalOpen, setModalOpen] = useState(false);

  return (
    <div className="relative">
      <MinerStatusWidget onClick={() => setModalOpen(true)} summary={summary} circle={circle} />

      {/* ProtoOS StatusModal handles both WakingDialog and StatusModal internally */}

      <ProtoOSStatusModal open={isModalOpen} onClose={() => setModalOpen(false)} />
    </div>
  );
};

export default MinerStatus;
