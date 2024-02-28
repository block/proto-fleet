import ControllerIpAddressInfo, { ControllerIpAddressInfoProps } from ".";

const InfoItemWrapper = ({ loading, ipAddress }: ControllerIpAddressInfoProps) => {
  return (
    <div className="w-64">
      <ControllerIpAddressInfo loading={loading} ipAddress={ipAddress} />
    </div>
  );
};

export const Default = () => {
  return <InfoItemWrapper ipAddress="42.08.59.58.84.c6" />;
};

export const Loading = () => {
  return <InfoItemWrapper loading />;
};

export const Error = () => {
  return <InfoItemWrapper />;
};

export default {
  title: "Navigation Sidebar/Info Items/Controller IP Address Info",
};
