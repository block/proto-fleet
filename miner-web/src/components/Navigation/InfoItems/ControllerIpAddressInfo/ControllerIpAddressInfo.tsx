import InfoItem from "../InfoItem";

export interface ControllerIpAddressInfoProps {
  ipAddress?: string;
  loading?: boolean;
}

const ControllerIpAddressInfo = ({
  ipAddress,
  loading,
}: ControllerIpAddressInfoProps) => {
  return (
    <InfoItem
      label="Controller Board IP Address"
      value={ipAddress}
      loading={loading}
    />
  );
};

export default ControllerIpAddressInfo;
