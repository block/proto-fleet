import { useTestConnection } from "@/protoOS/api";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
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
    <PoolModal
      onChangePools={onChangePools}
      onDismiss={onDismiss}
      poolIndex={poolIndex}
      pools={pools}
      show={show}
      isDefault={false}
      isTestingConnection={isTestingConnection}
      testConnection={testConnection}
    />
  );
};

export default BackupPoolModalWrapper;
