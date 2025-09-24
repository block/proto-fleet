import { useState } from "react";

import MinerStatusWidget from "./MinerStatusWidget";
import { WakingDialog } from "@/protoOS/components/Power";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import { useSystemContext } from "@/protoOS/contexts/SystemContext";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import { type ButtonVariant } from "@/shared/components/Button";
import MinerStatusModal, {
  MinerStatus as MinerStatusType,
} from "@/shared/components/MinerStatusModal";

interface MinerStatusProps {
  status?: MinerStatusType;
  variant?: ButtonVariant;
}

const MinerStatus = ({ status, variant }: MinerStatusProps) => {
  const [showModal, setShowModal] = useState(false);
  const { miningStatus } = useMinerStatus();
  const { isProtoRig } = useSystemContext();

  const { wakeMiner, shouldWake } = useWakeMiner({
    miningStatus,
  });

  return (
    <div className="relative">
      <MinerStatusWidget
        onClick={() => setShowModal(true)}
        status={status}
        variant={variant}
      />
      {showModal && status && (
        <MinerStatusModal
          status={status}
          onDismiss={() => setShowModal(false)}
          isProtoRig={isProtoRig}
          onWake={() => {
            setShowModal(false);
            wakeMiner();
          }}
        />
      )}
      <WakingDialog show={shouldWake} />
    </div>
  );
};

export default MinerStatus;
