import { useEffect, useState } from "react";
import { useFirmwareUpdate, useSystemReboot } from "@/protoOS/api";
import { useSystemContext } from "@/protoOS/contexts/SystemContext";
import { useFirmwareUpdateContext } from "@/protoOS/features/firmwareUpdate/";
import { statusLabelFromUpdateStatus } from "@/protoOS/features/firmwareUpdate/utility";
import { SettingsSolid } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";

const CheckForUpdate = () => {
  const { updateStatus, installing } = useFirmwareUpdateContext();
  const { checkFirmwareUpdate, updateFirmware } = useFirmwareUpdate();
  const { reload: reloadSystemInfo, pending: systemInfoPending } =
    useSystemContext();
  const { rebootSystem, pending: rebootPending } = useSystemReboot();
  const [pendingUpdate, setPendingUpdate] = useState(false);

  // reset pending update if not installing
  useEffect(() => {
    if (!installing) {
      setPendingUpdate(false);
    }
  }, [installing]);

  const checkForUpdates = () => {
    checkFirmwareUpdate()
      .then(() => {
        reloadSystemInfo();
      })
      .catch((error) => {
        console.error("Error checking for firmware updates:", error);
      });
  };

  return (
    <>
      {installing ||
      updateStatus?.status === "available" ||
      updateStatus?.status === "installed" ? (
        <Header
          title={statusLabelFromUpdateStatus(updateStatus, true)}
          description={updateStatus?.message}
          icon={<SettingsSolid />}
          titleSize="text-emphasis-300"
          inline
          className="w-full items-center rounded-xl bg-surface-base p-3 shadow-100"
          buttons={[
            {
              text:
                updateStatus?.status === "available"
                  ? "Install"
                  : convertToSentenceCase(updateStatus?.status || ""),
              variant: "secondary",
              className: updateStatus?.status === "installed" ? "hidden" : "",
              disabled: installing || pendingUpdate,
              loading: installing || pendingUpdate,
              onClick: () => {
                setPendingUpdate(true);
                updateFirmware().then();
              },
            },
            {
              text: "Reboot",
              variant: "primary",
              className: updateStatus?.status === "installed" ? "" : "hidden",
              loading: rebootPending,
              onClick: () => {
                rebootSystem();
              },
            },
          ]}
        />
      ) : (
        <Button
          variant="secondary"
          size="compact"
          loading={systemInfoPending}
          onClick={() => checkForUpdates()}
        >
          Check for updates
        </Button>
      )}
    </>
  );
};

export default CheckForUpdate;
