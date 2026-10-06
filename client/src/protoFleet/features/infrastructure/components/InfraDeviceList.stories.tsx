import InfraDeviceList from "./InfraDeviceList";
import { mockInfraDevices } from "./stories/mockInfraDevices";

export default {
  title: "Proto Fleet/Infrastructure/InfraDeviceList",
  component: InfraDeviceList,
};

export const Default = () => <InfraDeviceList devices={mockInfraDevices} />;
