import InfoItem from "../InfoItem";

export interface ControllerIpAddressInfoProps {
  ip_address?: string;
  loading?: boolean;
}

const ControllerIpAddressInfo = ({
  ip_address,
  loading,
}: ControllerIpAddressInfoProps) => {
  return (
    <InfoItem
      label="Controller Board IP Address"
      value={ip_address}
      loading={loading}
    />
  );
};

export default ControllerIpAddressInfo;
