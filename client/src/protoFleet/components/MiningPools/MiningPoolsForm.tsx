import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  CreatePoolRequestSchema,
  DeletePoolRequestSchema,
  UpdatePoolRequestSchema,
} from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { Pool } from "@/protoFleet/api/generated/pools/v1/pools_pb";

import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import usePools from "@/protoFleet/api/usePools";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import PoolRow from "@/shared/components/MiningPools/PoolRow";
import { BackupPoolIndex, PoolIndex, PoolInfo as SharedPoolInfo } from "@/shared/components/MiningPools/types";
import { getEmptyPoolsInfo, isValidPool } from "@/shared/components/MiningPools/utility";
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

const MiningPoolsForm = ({ buttonLabel, onSaveRequested, onSaveDone, onSaveFailed }: MiningPoolsProps) => {
  const { pools: existingPools, createPool, updatePool, deletePool, validatePool, validatePoolPending } = usePools();

  const { refetch: refetchOnboardingStatus } = useOnboardedStatus();

  const [loading, setLoading] = useState(false);
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());

  // Initialize and sync pools from existingPools
  useEffect(() => {
    if (existingPools.length === 0) {
      return;
    }
    const currentPools = existingPools
      .sort((a, b) => Number(a.poolId) - Number(b.poolId))
      .map((pool: Pool) => ({
        ...pool,
        name: pool.poolName,
        password: "",
        // TODO: fix priority assignment
        priority: pool.poolId,
      }));
    const maxExistingPriority = Math.max(
      // TODO: fix priority assignment
      ...existingPools.map((pool: Pool) => Number(pool.poolId)),
    );
    const emptyPools = getEmptyPoolsInfo(maxExistingPriority).slice(existingPools.length);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- sync local pools draft with existingPools prop when it changes
    setPools([...currentPools, ...emptyPools]);
  }, [existingPools]);

  // 0 is the default pool, 1 and 2 are backup pools
  const [currentPoolIndex, setCurrentPoolIndex] = useState<PoolIndex | null>(null);

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
            poolName: pool.name || "",
            url: pool.url,
            username: pool.username,
            password: pool.password,
          },
        });
        return () => createPool({ createPoolRequest, onError: handleSaveError });
      } else {
        // update existing pool. Proto3 explicit presence on the
        // password wrapper means an empty-string value is "erase the
        // stored password," not "leave it unchanged" — only include
        // the field when the user actually typed something. Without
        // this guard, saving an unmodified existing pool would wipe
        // its encrypted password and break subsequent mining auth.
        const updatePoolRequest = create(UpdatePoolRequestSchema, {
          poolId: BigInt(pool.poolId),
          poolName: pool.name || "",
          url: pool.url,
          username: pool.username,
          ...(pool.password && pool.password.length > 0 ? { password: pool.password } : {}),
        });
        return () => updatePool({ updatePoolRequest, onError: handleSaveError });
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

  const savePool = useCallback(
    async (pool: PoolInfo, isPasswordSet: boolean) => {
      if (pool.poolId === undefined) {
        // create new pool — pass through whatever password the user
        // typed (empty string when none, which is fine on create).
        const createPoolRequest = create(CreatePoolRequestSchema, {
          poolConfig: {
            poolName: pool.name || "",
            url: pool.url,
            username: pool.username,
            ...(isPasswordSet ? { password: pool.password } : {}),
          },
        });
        await createPool({ createPoolRequest, onError: handleSaveError });
      } else {
        // update existing pool. The password wrapper has presence
        // semantics — passing "" erases the stored password — so only
        // include it when the user actually changed it.
        const updatePoolRequest = create(UpdatePoolRequestSchema, {
          poolId: BigInt(pool.poolId),
          poolName: pool.name || "",
          url: pool.url,
          username: pool.username,
          ...(isPasswordSet ? { password: pool.password } : {}),
        });
        await updatePool({ updatePoolRequest, onError: handleSaveError });
      }
    },
    [createPool, updatePool],
  );

  // TODO support connection test
  return (
    <div>
      <Header
        className="mb-3"
        title="Mining pool"
        description="Your hashrate will contribute to your default mining pool. Add backup pools in case your default pool fails."
        inline
      />
      <WarnDefaultPoolCallout onDismiss={() => setWarnDefaultPool(false)} show={warnDefaultPool} />

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

        <PoolModal
          open={currentPoolIndex !== null}
          key={currentPoolIndex}
          onChangePools={onChangePools}
          onDismiss={() => setCurrentPoolIndex(null)}
          poolIndex={(currentPoolIndex ?? 0) as BackupPoolIndex}
          pools={pools}
          isTestingConnection={validatePoolPending}
          testConnection={validatePool}
          onSave={savePool}
          disallowUsernameSeparator
        />
      </div>

      <Button variant="primary" className="mt-6 ml-auto" loading={loading} onClick={handleContinue}>
        {buttonLabel}
      </Button>
    </div>
  );
};

export default MiningPoolsForm;
