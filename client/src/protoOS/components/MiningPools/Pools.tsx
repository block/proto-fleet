import { useState } from "react";

import { useTestConnection } from "@/protoOS/api";
import BackupPoolModalWrapper from "@/protoOS/components/MiningPools/BackupPoolModalWrapper";
import Button, { sizes, variants } from "@/shared/components/Button";
import BackupPoolRow from "@/shared/components/MiningPools/BackupPoolRow";
import PoolForm from "@/shared/components/MiningPools/PoolForm";
import {
  BackupPoolIndex,
  PoolIndex,
  PoolInfo,
} from "@/shared/components/MiningPools/types";

interface PoolsProps {
  onChangePools: (pools: PoolInfo[]) => void;
  pools: PoolInfo[];
}

const Pools = ({ onChangePools, pools }: PoolsProps) => {
  // 0 is the default pool, 1 and 2 are backup pools
  const [currentPoolIndex, setCurrentPoolIndex] = useState<PoolIndex>(0);
  const [shouldTestConnection, setShouldTestConnection] = useState(false);
  const { testConnection, pending: isTestingConnection } = useTestConnection();

  return (
    <div>
      <div className="mb-4 flex items-center">
        <div className="grow text-heading-100 text-text-primary">
          Default pool
        </div>
        <Button
          text="Test connection"
          onClick={() => setShouldTestConnection(true)}
          loading={isTestingConnection}
          size={sizes.compact}
          variant={variants.secondary}
          testId="test-connection-button"
        />
      </div>

      <PoolForm
        poolIndex={0}
        pools={pools}
        onChangePools={onChangePools}
        shouldTestConnection={shouldTestConnection}
        setShouldTestConnection={setShouldTestConnection}
        isTestingConnection={isTestingConnection}
        testConnection={testConnection}
      />

      <div className="mt-10">
        <div className="mb-1 text-heading-100 text-text-primary">
          Backup pools
        </div>
        <div className="mb-3 text-300 text-text-primary-70">
          Backup pools will only be used if your default pool fails.
        </div>
        {[...Array(2)].map((_, index) => {
          const backupPoolIndex = (index + 1) as BackupPoolIndex;
          return (
            <BackupPoolRow
              key={backupPoolIndex}
              pools={pools}
              backupPoolIndex={backupPoolIndex}
              onClick={() => setCurrentPoolIndex(backupPoolIndex)}
              testId={`backup-pool-${backupPoolIndex}-add-button`}
            />
          );
        })}
      </div>

      {[...Array(2)].map((_, index) => {
        const backupPoolIndex = (index + 1) as BackupPoolIndex;
        return (
          <BackupPoolModalWrapper
            key={backupPoolIndex}
            onChangePools={onChangePools}
            onDismiss={() => setCurrentPoolIndex(0)}
            poolIndex={backupPoolIndex}
            pools={pools}
            show={currentPoolIndex === backupPoolIndex}
          />
        );
      })}
    </div>
  );
};

export default Pools;
