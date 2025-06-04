import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  CreatePoolRequestSchema,
  DeletePoolRequestSchema,
  UpdatePoolRequestSchema,
} from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { Pool } from "@/protoFleet/api/generated/pools/v1/pools_pb";

import usePools from "@/protoFleet/api/usePools";
import { useOnboardingContext } from "@/protoFleet/features/onboarding/contexts/OnboardingContext";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import PoolRow from "@/shared/components/MiningPools/PoolRow";
import {
  BackupPoolIndex,
  PoolIndex,
  PoolInfo as SharedPoolInfo,
} from "@/shared/components/MiningPools/types";
import {
  getEmptyPoolsInfo,
  isValidPool,
} from "@/shared/components/MiningPools/utility";
import { WarnDefaultPoolCallout } from "@/shared/components/MiningPools/WarnDefaultPoolCallout";

type PoolInfo = SharedPoolInfo & {
  poolId?: bigint;
};

interface MiningPoolsProps {
  buttonLabel: string;
  onSaveRequested?: () => void;
  onSaveDone: () => void;
  onSaveFailed?: () => void;
}

const MiningPoolsForm = ({
  buttonLabel,
  onSaveRequested,
  onSaveDone,
  onSaveFailed,
}: MiningPoolsProps) => {
  const {
    pools: existingPools,
    createPool,
    updatePool,
    deletePool,
    validatePool,
    validatePoolPending,
  } = usePools();

  const { refetch: refetchOnboardingStatus } = useOnboardingContext();

  useEffect(() => {
    if (existingPools.length !== 0) {
      const currentPools = existingPools
        .sort((a, b) =>
          // always move default pool to the front, then sort by pool priority (lower number = higher priority)
          a.isDefault ? -1 : b.isDefault ? 1 : a.poolPriority - b.poolPriority,
        )
        .map((pool: Pool) => ({
          ...pool,
          password: "",
          priority: pool.poolPriority,
        }));
      const maxExistingPriority = Math.max(
        ...existingPools.map((pool: Pool) => pool.poolPriority),
      );
      const emptyPools = getEmptyPoolsInfo(maxExistingPriority).slice(
        existingPools.length,
      );
      setPools([...currentPools, ...emptyPools]);
    }
  }, [existingPools]);

  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [loading, setLoading] = useState(false);

  // 0 is the default pool, 1 and 2 are backup pools
  const [currentPoolIndex, setCurrentPoolIndex] = useState<PoolIndex | null>(
    null,
  );

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);

  const handleSaveError = (error: string) => {
    // TODO better error handling
    console.error("Error saving pool:", error);
  };

  const handleContinue = useCallback(() => {
    // check if default pool has been entered
    const noValidDefaultPool = !isValidPool(pools[0]);
    if (noValidDefaultPool) {
      setWarnDefaultPool(true);
      return;
    }

    onSaveRequested?.();
    setLoading(true);

    const validPools = pools.filter((pool) => isValidPool(pool));
    const apiCalls = validPools.map((pool) => {
      if (pool.poolId === undefined) {
        // create new pool
        const createPoolRequest = create(CreatePoolRequestSchema, {
          poolConfig: {
            url: pool.url,
            username: pool.username,
            password: pool.password,
          },
        });
        return () =>
          createPool({ createPoolRequest, onError: handleSaveError });
      } else {
        // update existing pool
        const updatePoolRequest = create(UpdatePoolRequestSchema, {
          poolId: BigInt(pool.poolId),
          url: pool.url,
          username: pool.username,
          password: pool.password,
        });
        return () =>
          updatePool({ updatePoolRequest, onError: handleSaveError });
      }
    });

    // handle deleted pools
    existingPools.forEach((pool) => {
      // intentionally convert bigint to number for comparison
      const foundPool = validPools.find((p) => p.poolId == pool.poolId);
      if (foundPool === undefined) {
        // delete pool
        const deletePoolRequest = create(DeletePoolRequestSchema, {
          poolId: pool.poolId,
        });
        apiCalls.push(() => {
          return deletePool({ deletePoolRequest, onError: handleSaveError });
        });
      }
    });

    apiCalls[0]().then(() => {
      // wait for default pool to be saved before saving backup pools
      const promises = apiCalls.slice(1).map((apiCall) => {
        return apiCall();
      });
      Promise.all(promises)
        .then(async () => {
          await refetchOnboardingStatus();
          onSaveDone();
        })
        .catch(() => onSaveFailed?.())
        .finally(() => {
          setLoading(false);
        });
    });
  }, [
    pools,
    onSaveRequested,
    existingPools,
    createPool,
    updatePool,
    deletePool,
    onSaveDone,
    onSaveFailed,
    refetchOnboardingStatus,
  ]);

  const onChangePools = useCallback((newPools: PoolInfo[]) => {
    setPools(newPools);
    if (isValidPool(newPools[0])) {
      setWarnDefaultPool(false);
    }
  }, []);

  // TODO support connection test
  return (
    <div>
      <Header
        className="mb-3"
        title="Mining pool"
        description="Your hashrate will contribute to your default mining pool. Add backup pools in case your default pool fails."
        inline
      />
      <WarnDefaultPoolCallout
        onDismiss={() => setWarnDefaultPool(false)}
        show={warnDefaultPool}
      />

      <div>
        {[...Array(3)].map((_, index) => {
          const poolIndex = index as PoolIndex;
          return (
            <PoolRow
              key={poolIndex}
              pools={pools}
              poolIndex={poolIndex}
              title={index === 0 ? "Default pool" : "Backup pool #" + poolIndex}
              onClick={() => setCurrentPoolIndex(poolIndex)}
              testId={`pool-${poolIndex}-add-button`}
            />
          );
        })}

        {[...Array(3)].map((_, index) => {
          const poolIndex = index as BackupPoolIndex;
          return (
            <PoolModal
              key={poolIndex}
              onChangePools={onChangePools}
              onDismiss={() => setCurrentPoolIndex(null)}
              poolIndex={poolIndex}
              pools={pools}
              show={currentPoolIndex === poolIndex}
              isDefault={index === 0}
              isTestingConnection={validatePoolPending}
              testConnection={validatePool}
            />
          );
        })}
      </div>

      <Button
        variant="primary"
        className="mt-6 ml-auto"
        loading={loading}
        onClick={handleContinue}
      >
        {buttonLabel}
      </Button>
    </div>
  );
};

export default MiningPoolsForm;
