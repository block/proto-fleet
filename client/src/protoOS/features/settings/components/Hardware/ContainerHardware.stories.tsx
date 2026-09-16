import { ContainerHardwareView } from "./ContainerHardware";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asDeviceId } from "@/protoOS/store/contract";

export const Container = () => {
  const { hardware } = tanksFixture(6, 16);
  for (const [id, board] of hardware.hashboards) {
    hardware.hashboards.set(id, { ...board, boardModel: "F1MC3", firmware: { version: "0.12.4" } });
  }
  return (
    <ContainerHardwareView
      hardware={hardware}
      systemInfo={{
        identity: {
          deviceId: asDeviceId("device:CT1-01"),
          deviceType: "container",
          adapterId: "mdk-v2",
          macAddresses: [],
        },
        moduleModel: "ct1",
        controlBoard: { model: "Proto controller", machineName: "Control server 1", serialNumber: "CB-001" },
        operatingSystem: { version: "2.0.0" },
      }}
    />
  );
};

export default { title: "Proto OS/Settings/Container hardware", component: ContainerHardwareView };
