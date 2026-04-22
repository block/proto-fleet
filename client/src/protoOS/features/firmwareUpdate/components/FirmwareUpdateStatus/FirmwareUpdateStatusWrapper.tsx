import { useEffect, useState } from "react";
import FirmwareUpdateStatus from "./FirmwareUpdateStatus";
import { useSystemReboot } from "@/protoOS/api";
import { useFirmwareUpdate } from "@/protoOS/api";
import {
  useDismissedLoginModal,
  useFirmwareUpdateInstalling,
  useFwUpdateStatus,
  useSetDismissedLoginModal,
  useSetPausedAuthAction,
  useSystemInfoPending,
} from "@/protoOS/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

const FirmwareUpdateStatusWrapper = () => {
  const { rebootSystem, pending: rebootPending } = useSystemReboot();
  const { updateFirmware } = useFirmwareUpdate();
  const installing = useFirmwareUpdateInstalling();
  const updateStatus = useFwUpdateStatus();
  const pending = useSystemInfoPending();
  const dismissedLoginModal = useDismissedLoginModal();
  const setDismissedLoginModal = useSetDismissedLoginModal();
  const setPausedAuthAction = useSetPausedAuthAction();

  const [updatePending, setUpdatePending] = useState(false);

  useEffect(() => {
    if (dismissedLoginModal) {
      setUpdatePending(false);
      setDismissedLoginModal(false);
      setPausedAuthAction(null);
    }

    return () => {
      setUpdatePending(false);
    };
  }, [dismissedLoginModal, setDismissedLoginModal, setPausedAuthAction]);

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
      setUpdatePending(false);
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
