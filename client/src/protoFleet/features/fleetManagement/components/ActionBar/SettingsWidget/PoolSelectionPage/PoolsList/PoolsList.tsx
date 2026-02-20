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
  | { status: "idle"; poolId?: undefined }
  | { status: "validating"; poolId: string; pool: MiningPool }
  | { status: "valid"; poolId: string; pool: MiningPool }
  | { status: "error"; poolId: string; pool: MiningPool; error: string };

interface MiningPoolsListProps {
  title: string;
  subtitle: string;
  onSelect: (poolId: string) => void;
  createNewLabel: string;
  poolNumber?: number;
  excludedPoolIds?: (string | undefined)[];
  testId?: string;
  disabled?: boolean;
  selectedPoolId?: string;
}

const PoolsList = ({
  title,
  subtitle,
  onSelect,
  createNewLabel,
  poolNumber,
  excludedPoolIds = [],
  testId,
  disabled = false,
  selectedPoolId,
}: MiningPoolsListProps) => {
  const [showSelectionModal, setShowSelectionModal] = useState(false);
  const [poolState, setPoolState] = useState<PoolSelectionState>({ status: "idle" });

  const { validatePool, miningPools } = usePools();

  const findPoolById = (poolId: string): MiningPool | undefined => {
    return miningPools.find((p) => p.poolId === poolId);
  };

  // Derive effective state: if parent's selectedPoolId doesn't match our poolState's poolId, treat as idle
  const isStateValid = poolState.status !== "idle" && poolState.poolId === selectedPoolId;

  // Get the selected pool - either from our local state (during validation) or from the pools list (for pre-populated selections)
  const selectedPool = isStateValid ? poolState.pool : selectedPoolId ? (findPoolById(selectedPoolId) ?? null) : null;

  const isTestingConnection = isStateValid && poolState.status === "validating";
  const poolError = isStateValid && poolState.status === "error" ? poolState.error : null;

  const displayError = poolError;

  const handlePoolSelect = (newPoolId: string, newPool?: MiningPool) => {
    // Use newPool if provided (e.g., from pool creation flow) to avoid race condition.
    // When a pool is created, setState is async so the pool may not be in miningPools yet.
    const pool = newPool ?? findPoolById(newPoolId);
    if (!pool) return;

    setPoolState({ status: "validating", poolId: newPoolId, pool });
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
          setPoolState({ status: "error", poolId: newPoolId, pool, error: "Connection failed" });
        } else {
          setPoolState({ status: "valid", poolId: newPoolId, pool });
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

  return (
    <>
      <div
        className={`flex flex-col rounded-xl border border-border-10 p-4 ${disabled ? "opacity-50" : ""}`}
        data-testid={testId}
        aria-disabled={disabled}
      >
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
              {selectedPool ? (
                <p className="text-text-secondary text-300">
                  <span className="text-text-primary">Configured pool:</span>{" "}
                  {selectedPool.name || selectedPool.poolUrl}
                </p>
              ) : subtitle ? (
                <p className="text-text-secondary text-300">{subtitle}</p>
              ) : null}
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
            <Button
              text="Update"
              variant={variants.secondary}
              size={sizes.base}
              onClick={handleUpdate}
              disabled={disabled}
            />
          ) : (
            <Button
              text={createNewLabel}
              variant={variants.secondary}
              size={sizes.base}
              onClick={() => setShowSelectionModal(true)}
              disabled={disabled}
            />
          )}
        </div>
      </div>

      {showSelectionModal ? (
        <PoolSelectionModal
          onDismiss={() => setShowSelectionModal(false)}
          onSave={handlePoolSelect}
          excludedPoolIds={excludedPoolIds}
        />
      ) : null}
    </>
  );
};

export default PoolsList;
