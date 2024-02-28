import ControllerMacAddressInfo, { ControllerMacAddressInfoProps } from ".";

const InfoItemWrapper = ({ loading, macAddress }: ControllerMacAddressInfoProps) => {
  return (
    <div className="w-64">
      <ControllerMacAddressInfo loading={loading} macAddress={macAddress} />
    </div>
  );
};

export const Default = () => {
  return <InfoItemWrapper macAddress="42.08.59.58.84.c6" />;
};

export const Loading = () => {
  return <InfoItemWrapper loading />;
};

export const Error = () => {
  return <InfoItemWrapper />;
};

export default {
  title: "Navigation Sidebar/Info Items/Controller Mac Address Info",
};
