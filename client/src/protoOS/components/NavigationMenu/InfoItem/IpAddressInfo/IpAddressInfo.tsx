import InfoItem from "../InfoItem";

export interface IpAddressInfoProps {
  loading?: boolean;
  value?: string;
}

const IpAddressInfo = ({ loading, value }: IpAddressInfoProps) => {
  return (
    <InfoItem
      label="IP Address"
      loading={loading && !value}
      value={value}
      divider={false}
      testId="ip-address-info-item"
    />
  );
};

export default IpAddressInfo;
