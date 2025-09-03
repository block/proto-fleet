import { useState } from "react";
import FirmwareUpdateStatus from "./FirmwareUpdateStatus";
import { useSystemReboot } from "@/protoOS/api";
import { useFirmwareUpdate } from "@/protoOS/api";
import { useFirmwareUpdateContext } from "@/protoOS/features/firmwareUpdate/contexts/FirmwareUpdateContext/";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
} from "@/shared/features/toaster";

const FirmwareUpdateStatusWrapper = () => {
  const { rebootSystem, pending: rebootPending } = useSystemReboot();
  const { updateFirmware } = useFirmwareUpdate();
  const { installing, updateStatus, pending } = useFirmwareUpdateContext();

  const [updatePending, setUpdatePending] = useState(false);

  const handleReboot = () => {
    rebootSystem({
      onSuccess: () => {
        pushToast({
          message: "Rebooting system...",
          status: TOAST_STATUSES.queued,
        });
      },
      onError: (error) => {
        console.error(error);
        pushToast({
          message: "Reboot failed. Please try again.",
          status: TOAST_STATUSES.error,
        });
      },
    });
  };

  const handleUpdate = async () => {
    try {
      setUpdatePending(true);
      await updateFirmware();
      pushToast({
        message: "Updating system...",
        status: TOAST_STATUSES.queued,
      });
    } catch (error: any) {
      console.error(error);
      pushToast({
        message: "Update failed. Please try again.",
        status: TOAST_STATUSES.error,
      });
    }
  };

  return (
    <FirmwareUpdateStatus
      updateStatus={updateStatus}
      installing={installing}
      loading={pending}
      rebootPending={rebootPending}
      onReboot={handleReboot}
      onUpdate={handleUpdate}
      updatePending={updatePending}
    />
  );
};

export default FirmwareUpdateStatusWrapper;
