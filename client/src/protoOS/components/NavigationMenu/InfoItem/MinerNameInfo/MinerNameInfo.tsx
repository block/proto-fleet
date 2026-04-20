import InfoItem from "../InfoItem";

export interface MinerNameInfoProps {
  loading?: boolean;
  value?: string;
}

const MinerNameInfo = ({ loading, value }: MinerNameInfoProps) => {
  return (
    <InfoItem label="Miner" loading={loading && !value} value={value} divider={false} testId="miner-name-info-item" />
  );
};

export default MinerNameInfo;
