import { useState } from "react";

import MinerStatusWidget from "./MinerStatusWidget";
import { WakingDialog, WarnWakeDialog } from "@/protoOS/components/Power";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import MinerStatusModal, {
  MinerStatus as MinerStatusType,
} from "@/shared/components/MinerStatusModal";

interface MinerStatusProps {
  status?: MinerStatusType;
}

const MinerStatus = ({ status }: MinerStatusProps) => {
  const [showModal, setShowModal] = useState(false);
  const { miningStatus } = useMinerStatus();

  const {
    wakeMiner,
    warnWake,
    shouldWake,
    handleWakeConfirm,
    onWarnWakeClose,
  } = useWakeMiner({
    miningStatus,
  });

  return (
    <div className="relative">
      <MinerStatusWidget onClick={() => setShowModal(true)} status={status} />
      {showModal && status && (
        <MinerStatusModal
          status={status}
          onDismiss={() => setShowModal(false)}
          onWake={() => {
            setShowModal(false);
            wakeMiner();
          }}
        />
      )}
      <WarnWakeDialog
        onClose={onWarnWakeClose}
        onSubmit={handleWakeConfirm}
        show={warnWake}
      />
      <WakingDialog show={shouldWake} />
    </div>
  );
};

export default MinerStatus;
