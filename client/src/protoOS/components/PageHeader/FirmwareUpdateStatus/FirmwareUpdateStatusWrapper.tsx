import { useState } from "react";
import FirmwareUpdateStatus from "./FirmwareUpdateStatus";
import { useFirmwareUpdate, useSystemReboot } from "@/protoOS/api";
import { useSystemInfo } from "@/protoOS/api/useSystemInfo";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
} from "@/shared/features/toaster";

const FirmwareUpdateStatusWrapper = () => {
  const { data: systemInfo, pending } = useSystemInfo({ poll: true });
  const { rebootSystem, pending: rebootPending } = useSystemReboot();
  const { updateFirmware } = useFirmwareUpdate({ poll: false });

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
        pushToast({
          message: error?.error?.message ?? "Reboot failed. Please try again.",
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
      pushToast({
        message: error?.error?.message ?? "Update failed. Please try again.",
        status: TOAST_STATUSES.error,
      });
    } finally {
      setUpdatePending(false);
    }
  };

  return (
    <FirmwareUpdateStatus
      updateStatus={systemInfo?.sw_update_status}
      loading={pending}
      rebootPending={rebootPending}
      onReboot={handleReboot}
      onUpdate={handleUpdate}
      updatePending={updatePending}
    />
  );
};

export default FirmwareUpdateStatusWrapper;
