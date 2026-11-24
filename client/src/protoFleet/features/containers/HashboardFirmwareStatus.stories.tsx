import { create } from "@bufbuild/protobuf";
import HashboardFirmwareStatus from "./HashboardFirmwareStatus";
import { HashboardReadingSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

export default { title: "Proto Fleet/Containers/Hashboard Firmware Status", component: HashboardFirmwareStatus };

export const Flashing = () => (
  <HashboardFirmwareStatus
    board={create(HashboardReadingSchema, {
      metadata: {
        firmwareUpdate: {
          status: "FIRMWARE_TARGET_STATUS_FLASHING",
          progressPercent: 42,
          serverId: "ct1-controller-b",
          observedAt: new Date().toISOString(),
        },
      },
    })}
  />
);

export const Failed = () => (
  <HashboardFirmwareStatus
    board={create(HashboardReadingSchema, {
      metadata: {
        firmwareUpdate: {
          status: "FIRMWARE_TARGET_STATUS_FAILED",
          serverId: "ct1-controller-a",
          message: "Hashboard did not report the expected version after reboot.",
          observedAt: new Date().toISOString(),
        },
      },
    })}
  />
);
