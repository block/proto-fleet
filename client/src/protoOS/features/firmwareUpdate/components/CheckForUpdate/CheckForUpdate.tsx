import { useFirmwareUpdate, useSystemInfo, useSystemReboot } from "@/protoOS/api";
import { statusLabelFromUpdateStatus } from "@/protoOS/features/firmwareUpdate/utility";
import { useFirmwareUpdateInstalling, useFwUpdateStatus, useSystemInfoPending } from "@/protoOS/store";
import { SettingsSolid } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";

const CheckForUpdate = () => {
  const updateStatus = useFwUpdateStatus();
  const installing = useFirmwareUpdateInstalling();
  const { checkFirmwareUpdate, updateFirmware, pendingUpdate } = useFirmwareUpdate();
  const systemInfoPending = useSystemInfoPending();
  const { reload: reloadSystemInfo } = useSystemInfo({ poll: false });
  const { rebootSystem, pending: rebootPending } = useSystemReboot();

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
      {installing || updateStatus?.status === "available" || updateStatus?.status === "installed" ? (
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
                updateStatus?.status === "available" ? "Install" : convertToSentenceCase(updateStatus?.status || ""),
              variant: "secondary",
              className: updateStatus?.status === "installed" ? "hidden" : "",
              disabled: installing || pendingUpdate,
              loading: installing || pendingUpdate,
              onClick: () => {
                updateFirmware();
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
        <Button variant="secondary" size="compact" loading={systemInfoPending} onClick={() => checkForUpdates()}>
          Check for updates
        </Button>
      )}
    </>
  );
};

export default CheckForUpdate;
