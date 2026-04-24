import InfoItem from "../InfoItem";

export interface VersionInfoProps {
  loading?: boolean;
  value?: string;
}

const VersionInfo = ({ loading, value }: VersionInfoProps) => {
  return (
    <InfoItem
      label="Firmware Version"
      loading={loading ? !value : false}
      value={value}
      divider={false}
      testId="version-info-item"
    />
  );
};

export default VersionInfo;
