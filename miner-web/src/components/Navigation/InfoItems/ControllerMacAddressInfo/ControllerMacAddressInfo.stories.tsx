import ControllerMacAddressInfo, { ControllerMacAddressInfoProps } from ".";

const InfoItemWrapper = ({ loading, mac_address }: ControllerMacAddressInfoProps) => {
  return (
    <div className="w-64">
      <ControllerMacAddressInfo loading={loading} mac_address={mac_address} />
    </div>
  );
};

export const Default = () => {
  return <InfoItemWrapper mac_address="42.08.59.58.84.c6" />;
};

export const Loading = () => {
  return <InfoItemWrapper loading />;
};

export const Error = () => {
  return <InfoItemWrapper />;
};

export default {
  component: ControllerMacAddressInfo,
  title: "Navigation Sidebar/Info Items/Controller Mac Address Info",
};
