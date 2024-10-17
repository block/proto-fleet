import { useState } from "react";

import { ErrorListResponse } from "apiTypes";

import MinerStatusModal from "../../MinerStatusModal/MinerStatusModal";
import MinerStatusWidget from "./MinerStatusWidget";

interface MinerStatusProps {
  errors?: ErrorListResponse;
  loading?: boolean;
}

const MinerStatus = ({ errors, loading = false }: MinerStatusProps) => {
  const [showModal, setShowModal] = useState(false);

  return (
    <div className="relative">
      <MinerStatusWidget
        errors={errors}
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
