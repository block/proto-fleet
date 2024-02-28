import { useState } from "react";

import { useTestConnection } from "api";

import Button, { sizes, variants } from "components/Button";

import { BackupPoolIndex, PoolIndex, PoolInfo } from "../types";
import BackupPoolModal from "./BackupPoolModal";
import BackupPoolRow from "./BackupPoolRow";
import PoolForm from "./PoolForm";

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
      <div className="flex items-center mb-4">
        <div className="text-heading-100 text-text-primary grow">
          Default pool
        </div>
        <Button
          text="Test connection"
          onClick={() => setShouldTestConnection(true)}
          loading={isTestingConnection}
          size={sizes.compact}
          variant={variants.secondary}
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
        <div className="text-heading-100 text-text-primary mb-1">
          Backup pools
        </div>
        <div className="text-300 text-text-primary/70 mb-3">
          Backup pools will only be used if your default pool fails.
        </div>
        {[...Array(2)].map((_, index) => {
          const backupPoolIndex = index + 1 as BackupPoolIndex;
          return (
            <BackupPoolRow
              key={backupPoolIndex}
              pools={pools}
              backupPoolIndex={backupPoolIndex}
              onClick={() => setCurrentPoolIndex(backupPoolIndex)}
            />
          );
        })}
      </div>

      {[...Array(2)].map((_, index) => {
        const backupPoolIndex = index + 1 as BackupPoolIndex;
        return (
          <BackupPoolModal
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
