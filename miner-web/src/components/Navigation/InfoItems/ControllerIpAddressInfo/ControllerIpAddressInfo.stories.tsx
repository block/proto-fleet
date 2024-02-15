import ControllerIpAddressInfo, { ControllerIpAddressInfoProps } from ".";

const InfoItemWrapper = ({ loading, ip_address }: ControllerIpAddressInfoProps) => {
  return (
    <div className="w-64">
      <ControllerIpAddressInfo loading={loading} ip_address={ip_address} />
    </div>
  );
};

export const Default = () => {
  return <InfoItemWrapper ip_address="42.08.59.58.84.c6" />;
};

export const Loading = () => {
  return <InfoItemWrapper loading />;
};

export const Error = () => {
  return <InfoItemWrapper />;
};

export default {
  component: ControllerIpAddressInfo,
  title: "Navigation Sidebar/Info Items/Controller IP Address Info",
};
