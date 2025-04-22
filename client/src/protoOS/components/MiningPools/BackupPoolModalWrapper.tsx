import { useTestConnection } from "@/protoOS/api";
import BackupPoolModal from "@/shared/components/MiningPools/BackupPoolModal";
import { PoolIndex, PoolInfo } from "@/shared/components/MiningPools/types";

interface BackupPoolPropsWrapper {
  onChangePools: (pools: PoolInfo[]) => void;
  onDismiss: () => void;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  show: boolean;
}

const BackupPoolModalWrapper = ({
  onChangePools,
  onDismiss,
  poolIndex,
  pools,
  show,
}: BackupPoolPropsWrapper) => {
  const { testConnection, pending: isTestingConnection } = useTestConnection();

  return (
    <BackupPoolModal
      onChangePools={onChangePools}
      onDismiss={onDismiss}
      poolIndex={poolIndex}
      pools={pools}
      show={show}
      isTestingConnection={isTestingConnection}
      testConnection={testConnection}
    />
  );
};

export default BackupPoolModalWrapper;
