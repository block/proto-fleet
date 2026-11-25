import PsuFirmwareStatus from "./PsuFirmwareStatus";

export default { title: "Proto Fleet/Containers/PSU Firmware Status", component: PsuFirmwareStatus };

const now = Date.now();
const observedAt = new Date(now).toISOString();

export const Updating = () => (
  <PsuFirmwareStatus
    now={now}
    update={{
      state: "PSU_FIRMWARE_UPDATE_STATE_APPLYING",
      progressPercent: 65,
      serverId: "ct1-controller-b",
      observedAt,
    }}
  />
);

export const RecoveryRequired = () => (
  <PsuFirmwareStatus
    now={now}
    update={{
      state: "PSU_FIRMWARE_UPDATE_STATE_FAILED",
      serverId: "ct1-controller-a",
      recoveryRequired: true,
      miningHeld: true,
      message: "PSU firmware update failed in PSU_FW_PHASE_RECOVERY_REQUIRED: PSU_FW_ERROR_I2C",
      observedAt,
    }}
  />
);
