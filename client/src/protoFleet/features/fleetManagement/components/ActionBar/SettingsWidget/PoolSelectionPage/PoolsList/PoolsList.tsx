import { useState } from "react";
import PoolSelectionModal from "../PoolSelectionModal/PoolSelectionModal";
import { MiningPool } from "../types";
import usePools from "@/protoFleet/api/usePools";
import MiningPools from "@/shared/assets/icons/MiningPools";
import Button from "@/shared/components/Button";
import { sizes, variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SlotNumber from "@/shared/components/SlotNumber/SlotNumber";

type PoolSelectionState =
  | { status: "idle" }
  | { status: "selected"; pool: MiningPool }
  | { status: "validating"; pool: MiningPool }
  | { status: "valid"; pool: MiningPool }
  | { status: "error"; pool: MiningPool; error: string };

interface MiningPoolsListProps {
  title: string;
  subtitle: string;
  onSelect: (poolId: string) => void;
  createNewLabel: string;
  poolNumber?: number;
  excludedPoolIds?: (string | undefined)[];
  testId?: string;
}

const PoolsList = ({
  title,
  subtitle,
  onSelect,
  createNewLabel,
  poolNumber,
  excludedPoolIds = [],
  testId,
}: MiningPoolsListProps) => {
  const [showSelectionModal, setShowSelectionModal] = useState(false);
  const [poolState, setPoolState] = useState<PoolSelectionState>({
    status: "idle",
  });

  const { validatePool, miningPools } = usePools();

  const findPoolById = (poolId: string): MiningPool | undefined => {
    return miningPools.find((p) => p.poolId === poolId);
  };

  const selectedPool = poolState.status !== "idle" ? poolState.pool : null;

  const isTestingConnection = poolState.status === "validating";

  const hasPoolConflict = selectedPool && excludedPoolIds.some((id) => id === selectedPool.poolId);

  const poolError = poolState.status === "error" ? poolState.error : null;

  const displayError = poolError || (hasPoolConflict ? "Duplicate pool selected" : null);

  const handlePoolSelect = (poolId: string, newPool?: MiningPool) => {
    // Use newPool if provided (e.g., from pool creation flow) to avoid race condition.
    // When a pool is created, setState is async so the pool may not be in miningPools yet.
    const pool = newPool ?? findPoolById(poolId);
    if (!pool) return;

    setPoolState({ status: "validating", pool });
    setShowSelectionModal(false);

    const minSpinnerDisplayTime = 800;
    const startTime = Date.now();

    const withMinimumDelay = (callback: () => void) => {
      const elapsed = Date.now() - startTime;
      const remainingTime = Math.max(0, minSpinnerDisplayTime - elapsed);
      setTimeout(callback, remainingTime);
    };

    const finishTesting = (error?: string) => {
      withMinimumDelay(() => {
        if (error) {
          console.error(error);
          setPoolState({ status: "error", pool, error: "Connection failed" });
        } else {
          setPoolState({ status: "valid", pool });
        }
        onSelect(pool.poolId);
      });
    };

    validatePool({
      poolInfo: {
        url: pool.poolUrl,
        username: pool.username,
      },
      onSuccess: () => finishTesting(),
      onError: (error) => finishTesting(error),
    });
  };

  const handleUpdate = () => {
    setShowSelectionModal(true);
  };

  const displaySubtitle = selectedPool ? selectedPool.name || selectedPool.poolUrl : subtitle;

  return (
    <>
      <div className="flex flex-col rounded-xl border border-border-10 p-4" data-testid={testId}>
        {/* Header */}
        <div className="mb-4 flex flex-col gap-3">
          {/* Icon */}
          <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-surface-5">
            {poolNumber !== undefined ? <SlotNumber number={poolNumber} /> : <MiningPools className="h-5 w-5" />}
          </div>

          {/* Title */}
          <div className="flex-1">
            <h3 className="text-heading-300 text-text-primary">{title}</h3>
            <div className="mt-1 h-10">
              {displaySubtitle ? <p className="text-body-300 text-text-secondary">{displaySubtitle}</p> : null}
              {displayError ? <p className="text-300 text-intent-critical-fill">{displayError}</p> : null}
            </div>
          </div>
        </div>

        {/* Button or Testing Connection */}
        <div className="flex h-10 justify-end">
          {isTestingConnection ? (
            <div className="flex items-center gap-2">
              <ProgressCircular size={16} indeterminate />
              <span className="text-text-secondary text-300">Testing connection</span>
            </div>
          ) : selectedPool ? (
            <Button text="Update" variant={variants.secondary} size={sizes.base} onClick={handleUpdate} />
          ) : (
            <Button
              text={createNewLabel}
              variant={variants.secondary}
              size={sizes.base}
              onClick={() => setShowSelectionModal(true)}
            />
          )}
        </div>
      </div>

      {showSelectionModal ? (
        <PoolSelectionModal onDismiss={() => setShowSelectionModal(false)} onSave={handlePoolSelect} />
      ) : null}
    </>
  );
};

export default PoolsList;
