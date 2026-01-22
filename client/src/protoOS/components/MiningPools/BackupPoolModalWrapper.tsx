import { useTestConnection } from "@/protoOS/api";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import { PoolIndex, PoolInfo } from "@/shared/components/MiningPools/types";

interface BackupPoolPropsWrapper {
  onChangePools: (pools: PoolInfo[]) => void;
  onDismiss: () => void;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  show: boolean;
  mode?: "add" | "edit";
  onDelete?: () => void;
}

const BackupPoolModalWrapper = ({
  onChangePools,
  onDismiss,
  poolIndex,
  pools,
  show,
  mode = "add",
  onDelete,
}: BackupPoolPropsWrapper) => {
  const { testConnection, pending: isTestingConnection } = useTestConnection();

  return (
    <PoolModal
      onChangePools={onChangePools}
      onDismiss={onDismiss}
      poolIndex={poolIndex}
      pools={pools}
      show={show}
      isTestingConnection={isTestingConnection}
      testConnection={testConnection}
      mode={mode}
      onDelete={onDelete}
    />
  );
};

export default BackupPoolModalWrapper;
