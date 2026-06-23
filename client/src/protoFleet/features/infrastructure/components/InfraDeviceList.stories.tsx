import InfraDeviceList from "./InfraDeviceList";
import { mockDiscoveredInfraDevices, mockInfraDevices } from "./stories/mockInfraDevices";

export default {
  title: "Proto Fleet/Infrastructure/InfraDeviceList",
  component: InfraDeviceList,
};

export const Default = () => (
  <InfraDeviceList discoveredDevices={mockDiscoveredInfraDevices} devices={mockInfraDevices} />
);
