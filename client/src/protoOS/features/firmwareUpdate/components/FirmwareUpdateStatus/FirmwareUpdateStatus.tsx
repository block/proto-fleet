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
  const firmwareStatusMessage = useMemo(() => {
    return statusLabelFromUpdateStatus(updateStatus);
  }, [updateStatus]);

  return (
    <div
      className={clsx("relative", {
        hidden: !updateStatus || updateStatus.status === "current" || firmwareStatusMessage === undefined,
      })}
    >
      <FirmwareUpdateStatusWidget
        updateStatus={updateStatus}
        statusMessage={firmwareStatusMessage}
        installing={installing}
        loading={loading}
        onClick={() => setShowModal(true)}
      />
      <FirmwareUpdateStatusModal
        open={showModal}
        updateStatus={updateStatus}
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
