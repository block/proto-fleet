import { useState } from "react";

import MinerStatusModal from "../../MinerStatusModal/MinerStatusModal";
import MinerStatusWidget from "./MinerStatusWidget";
import {
  ErrorListResponse,
  MiningStatusMiningstatus,
} from "@/protoOS/api/types";

interface MinerStatusProps {
  errors?: ErrorListResponse;
  miningStatus?: MiningStatusMiningstatus;
  loading?: boolean;
}

const MinerStatus = ({
  errors,
  miningStatus,
  loading = false,
}: MinerStatusProps) => {
  const [showModal, setShowModal] = useState(false);

  return (
    <div className="relative">
      <MinerStatusWidget
        errors={errors}
        miningStatus={miningStatus}
        loading={loading && !errors?.length}
        onClick={() => setShowModal(true)}
      />
      {showModal && (
        <MinerStatusModal
          errors={errors}
          onDismiss={() => setShowModal(false)}
        />
      )}
    </div>
  );
};

export default MinerStatus;
