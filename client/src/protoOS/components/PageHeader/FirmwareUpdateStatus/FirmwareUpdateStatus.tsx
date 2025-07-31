import { useState } from "react";

import FirmwareUpdateStatusWidget from "./FirmwareUpdateStatusWidget";
import type { UpdateStatus } from "@/protoOS/api/types";
import { FirmwareUpdateStatusModal } from "@/protoOS/components/FirmwareUpdateStatusModal";

interface FirmwareUpdateStatusProps {
  updateStatus?: UpdateStatus;
  loading?: boolean;
  rebootPending?: boolean;
  updatePending?: boolean;
  onReboot?: () => void;
  onUpdate?: () => void;
}

const FirmwareUpdateStatus = ({
  updateStatus,
  loading = false,
  rebootPending = false,
  updatePending = false,
  onReboot,
  onUpdate,
}: FirmwareUpdateStatusProps) => {
  const [showModal, setShowModal] = useState(false);

  return (
    <div className="relative">
      <FirmwareUpdateStatusWidget
        updateStatus={updateStatus}
        loading={loading}
        onClick={() => setShowModal(true)}
      />
      {showModal && (
        <FirmwareUpdateStatusModal
          updateStatus={updateStatus}
          onReboot={onReboot}
          rebootPending={rebootPending}
          onDismiss={() => setShowModal(false)}
          show={showModal}
          onUpdate={onUpdate}
          updatePending={updatePending}
        />
      )}
    </div>
  );
};

export default FirmwareUpdateStatus;
