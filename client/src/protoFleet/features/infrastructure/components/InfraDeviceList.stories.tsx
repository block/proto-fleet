import InfraDeviceList from "./InfraDeviceList";
import { mockInfraDevices } from "@/protoFleet/features/infrastructure/mockInfraDevices";

export default {
  title: "Proto Fleet/Infrastructure/InfraDeviceList",
  component: InfraDeviceList,
};

export const Default = () => <InfraDeviceList devices={mockInfraDevices} />;
