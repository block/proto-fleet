import { useCallback, useEffect, useState } from "react";

import { useTestConnection } from "@/protoOS/api";
import BackupPoolModalWrapper from "@/protoOS/components/MiningPools/BackupPoolModalWrapper";
import Button, { sizes, variants } from "@/shared/components/Button";
import PoolForm from "@/shared/components/MiningPools/PoolForm";
import PoolRow from "@/shared/components/MiningPools/PoolRow";
import {
  BackupPoolIndex,
  PoolIndex,
  PoolInfo,
} from "@/shared/components/MiningPools/types";
import { debounce, deepClone } from "@/shared/utils/utility";

interface PoolsProps {
  onChangePools: (pools: PoolInfo[]) => void;
  pools: PoolInfo[];
}

const Pools = ({ onChangePools, pools }: PoolsProps) => {
  // create a local copy, since pools are being polled and the prop is changing often
  const [localPools, setLocalPools] = useState<PoolInfo[]>(deepClone(pools));
  const [isEditing, setIsEditing] = useState(false);

  // 0 is the default pool, 1 and 2 are backup pools
  const [currentPoolIndex, setCurrentPoolIndex] = useState<PoolIndex>(0);
  const [shouldTestConnection, setShouldTestConnection] = useState(false);
  const { testConnection, pending: isTestingConnection } = useTestConnection();

  const handlePoolsChange = useCallback(
    (pools: PoolInfo[]) => {
      setLocalPools(pools);
      onChangePools(pools);
    },
    [setLocalPools, onChangePools],
  );

  useEffect(() => {
    if (!isEditing) {
      setLocalPools(deepClone(pools));
    }
  }, [isEditing, pools]);

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const debouncedEditDone = useCallback(
    debounce(() => {
      setIsEditing(false);
    }),
    [setIsEditing],
  );

  const startEditing = useCallback(() => {
    // user is editing again, cancel debounce of editing done
    debouncedEditDone.cancel();
    setIsEditing(true);
  }, [debouncedEditDone]);

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
        pools={localPools}
        onChangePools={handlePoolsChange}
        shouldTestConnection={shouldTestConnection}
        setShouldTestConnection={setShouldTestConnection}
        isTestingConnection={isTestingConnection}
        testConnection={testConnection}
        onFocus={startEditing}
        onBlur={debouncedEditDone}
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
            <PoolRow
              key={backupPoolIndex}
              pools={localPools}
              poolIndex={backupPoolIndex}
              title={"Backup pool #" + backupPoolIndex}
              onClick={() => {
                startEditing();
                setCurrentPoolIndex(backupPoolIndex);
              }}
              testId={`pool-${backupPoolIndex}-add-button`}
            />
          );
        })}
      </div>

      {[...Array(2)].map((_, index) => {
        const backupPoolIndex = (index + 1) as BackupPoolIndex;
        return (
          <BackupPoolModalWrapper
            key={backupPoolIndex}
            onChangePools={(pools) => {
              debouncedEditDone();
              handlePoolsChange(pools);
            }}
            onDismiss={() => {
              debouncedEditDone();
              setCurrentPoolIndex(0);
            }}
            poolIndex={backupPoolIndex}
            pools={localPools}
            show={currentPoolIndex === backupPoolIndex}
          />
        );
      })}
    </div>
  );
};

export default Pools;
