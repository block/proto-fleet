import { useMemo, useState } from "react";

import clsx from "clsx";
import FirmwareUpdateStatusWidget from "./FirmwareUpdateStatusWidget";
import type { UpdateStatus } from "@/protoOS/api/generatedApi";
import FirmwareUpdateStatusModal from "@/protoOS/features/firmwareUpdate/components/FirmwareUpdateStatusModal";
import { statusLabelFromUpdateStatus } from "@/protoOS/features/firmwareUpdate/utility";

interface FirmwareUpdateStatusProps {
  updateStatus?: UpdateStatus;
  installing?: boolean;
  loading?: boolean;
  rebootPending?: boolean;
  updatePending?: boolean;
  onReboot?: () => void;
  onUpdate?: () => void;
}

const FirmwareUpdateStatus = ({
  updateStatus,
  installing = false,
  loading = false,
  rebootPending = false,
  updatePending = false,
  onReboot,
  onUpdate,
}: FirmwareUpdateStatusProps) => {
  const [showModal, setShowModal] = useState(false);
  // A `current` status can still carry the newest failed catalog check or
  // update attempt in `error`; surface it as an update error.
  const displayedUpdateStatus = useMemo<UpdateStatus | undefined>(() => {
    return updateStatus?.status === "current" && updateStatus.error
      ? { ...updateStatus, status: "error" }
      : updateStatus;
  }, [updateStatus]);
  const firmwareStatusMessage = useMemo(() => {
    return statusLabelFromUpdateStatus(displayedUpdateStatus);
  }, [displayedUpdateStatus]);

  return (
    <div
      className={clsx("relative", {
        hidden:
          !displayedUpdateStatus || displayedUpdateStatus.status === "current" || firmwareStatusMessage === undefined,
      })}
    >
      <FirmwareUpdateStatusWidget
        updateStatus={displayedUpdateStatus}
        statusMessage={firmwareStatusMessage}
        installing={installing}
        loading={loading}
        onClick={() => setShowModal(true)}
      />
      <FirmwareUpdateStatusModal
        open={showModal}
        updateStatus={displayedUpdateStatus}
        onReboot={onReboot}
        rebootPending={rebootPending}
        onDismiss={() => setShowModal(false)}
        onUpdate={onUpdate}
        updatePending={updatePending}
      />
    </div>
  );
};

export default FirmwareUpdateStatus;
