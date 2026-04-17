import { useTestConnection } from "@/protoOS/api/hooks/useTestConnection";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import { PoolIndex, PoolInfo } from "@/shared/components/MiningPools/types";

interface BackupPoolPropsWrapper {
  onChangePools: (pools: PoolInfo[]) => void;
  onDismiss: () => void;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  mode?: "add" | "edit";
  onDelete?: () => void;
  open?: boolean;
}

const BackupPoolModalWrapper = ({
  onChangePools,
  onDismiss,
  poolIndex,
  pools,
  mode = "add",
  onDelete,
  open,
}: BackupPoolPropsWrapper) => {
  const { testConnection, pending: isTestingConnection } = useTestConnection();

  return (
    <PoolModal
      open={open}
      onChangePools={onChangePools}
      onDismiss={onDismiss}
      poolIndex={poolIndex}
      pools={pools}
      isTestingConnection={isTestingConnection}
      testConnection={testConnection}
      mode={mode}
      onDelete={onDelete}
      usernameLabel="Username (optional)"
      usernameRequired={false}
      usernameHelperText={
        <>
          To add a worker name, add a period after the username followed by the worker name.
          <br />
          Example: mann23.workerbee
        </>
      }
    />
  );
};

export default BackupPoolModalWrapper;
