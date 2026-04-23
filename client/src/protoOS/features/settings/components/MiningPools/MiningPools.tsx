import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import clsx from "clsx";

import { STATUS_MESSAGES } from "./constants";
import { useCreatePools, useEditPool, usePoolsInfo } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import MiningPools, { getEmptyPoolsInfo, isValidPool, PoolInfo } from "@/protoOS/components/MiningPools";
import { Alert } from "@/shared/assets/icons";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import { pushToast, removeToast, STATUSES as TOAST_STATUSES, ToastStatusType } from "@/shared/features/toaster";
import { debounce } from "@/shared/utils/utility";

interface PoolChangeOptions {
  isDelete?: boolean;
}

const SettingsMiningPools = () => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [previousPools, setPreviousPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [toastStatus, setToastStatus] = useState<ToastStatusType | null>(null);
  const [isStalePools, setIsStalePools] = useState(false);
  const toastId = useRef<number | null>(null);
  const skipSuccessToastRef = useRef(false);

  const { data: poolsInfo, pending: poolsInfoPending, error: poolsInfoError } = usePoolsInfo();
  const { createPools } = useCreatePools();
  const { editPool } = useEditPool();
  const [createPoolsError, setCreatePoolsError] = useState<ErrorProps>();

  useEffect(() => {
    if (poolsInfo?.length) {
      const newPools = [...Array(3)].map((_, index) => {
        const pool = poolsInfo?.[index];
        return {
          name: pool?.name || "",
          url: pool?.url || "",
          username: pool?.user || "",
          password: "",
          priority: pool?.priority || index,
        };
      });
      setPools(newPools);
      setPreviousPools(newPools);
    }
  }, [poolsInfo]);

  const findChangedPool = useCallback((currentPools: PoolInfo[], previousPools: PoolInfo[]) => {
    let changedIndex = -1;
    let changesCount = 0;

    for (let i = 0; i < currentPools.length; i++) {
      const current = currentPools[i];
      const previous = previousPools[i];

      if (
        current.name !== previous.name ||
        current.url !== previous.url ||
        current.username !== previous.username ||
        current.password !== previous.password
      ) {
        changedIndex = i;
        changesCount++;
      }
    }

    // Return the index only if exactly one pool changed
    return changesCount === 1 ? changedIndex : -1;
  }, []);

  const submitPoolsRef = useRef<(newPools: PoolInfo[]) => void>(() => {});
  const submitPools = (newPools: PoolInfo[]) => {
    setToastStatus(TOAST_STATUSES.loading);
    removeToast(toastId.current);
    toastId.current = pushToast({
      message: STATUS_MESSAGES.loading,
      status: TOAST_STATUSES.loading,
    });

    const changedPoolIndex = findChangedPool(newPools, previousPools);
    const validPools = newPools.filter(isValidPool);

    // If only one pool changed and it exists in the server data, use edit
    if (changedPoolIndex >= 0 && poolsInfo?.[changedPoolIndex]) {
      const changedPool = newPools[changedPoolIndex];
      if (isValidPool(changedPool)) {
        editPool({
          poolId: changedPoolIndex,
          poolInfo: {
            name: changedPool.name,
            url: changedPool.url,
            username: changedPool.username,
            password: changedPool.password,
            priority: changedPool.priority,
          },
          onSuccess: () => {
            setCreatePoolsError(undefined);
            setIsStalePools(true);
            setPreviousPools(newPools);
          },
          onError: (error) => {
            setCreatePoolsError(error);
            setToastStatus(TOAST_STATUSES.error);
            removeToast(toastId.current);
            toastId.current = pushToast({
              message: STATUS_MESSAGES.error,
              status: TOAST_STATUSES.error,
            });
          },
          retryOnMinerDown: true,
        });
      } else {
        setToastStatus(TOAST_STATUSES.error);
        removeToast(toastId.current);
        toastId.current = pushToast({
          message: "Invalid pool configuration",
          status: TOAST_STATUSES.error,
        });
      }
    } else {
      // Multiple changes or new pools, use create (replace all)
      createPools({
        poolInfo: validPools,
        onSuccess: () => {
          setCreatePoolsError(undefined);
          setIsStalePools(true);
          setPreviousPools(newPools);
        },
        onError: (error) => {
          setCreatePoolsError(error);
          setToastStatus(TOAST_STATUSES.error);
          removeToast(toastId.current);
          toastId.current = pushToast({
            message: STATUS_MESSAGES.error,
            status: TOAST_STATUSES.error,
          });
        },
        retryOnMinerDown: true,
      });
    }
  };

  useEffect(() => {
    submitPoolsRef.current = submitPools;
  });

  // Stable debounced wrapper reads the latest submit impl via ref at fire time,
  // so pending submits always see current props/state (e.g. `previousPools` after
  // a successful edit) instead of a stale closure captured at schedule time.
  const debouncedSubmitPools = useMemo(
    // eslint-disable-next-line react-hooks/refs -- submitPoolsRef.current is read when the debounced callback fires (user input), not during render
    () => debounce((newPools: PoolInfo[]) => submitPoolsRef.current(newPools)),
    [],
  );

  useEffect(() => () => debouncedSubmitPools.cancel(), [debouncedSubmitPools]);

  useEffect(() => {
    if (toastStatus === TOAST_STATUSES.loading && isStalePools && !poolsInfoPending) {
      if (poolsInfoError && !/failed to connect to cgminer/i.test(poolsInfoError)) {
        setToastStatus(TOAST_STATUSES.error);
        removeToast(toastId.current);
        toastId.current = pushToast({
          message: STATUS_MESSAGES.error,
          status: TOAST_STATUSES.error,
        });

        setIsStalePools(false);
        skipSuccessToastRef.current = false;
      } else if (poolsInfo?.length !== undefined) {
        // Skip success toast if this was a delete operation (already showed "Pool removed")
        if (!skipSuccessToastRef.current) {
          setToastStatus(TOAST_STATUSES.success);
          removeToast(toastId.current);
          toastId.current = pushToast({
            message: STATUS_MESSAGES.success,
            status: TOAST_STATUSES.success,
          });
        } else {
          removeToast(toastId.current);
          setToastStatus(null);
        }
        setIsStalePools(false);
        skipSuccessToastRef.current = false;
      }
    }
  }, [isStalePools, poolsInfo, poolsInfoPending, poolsInfoError, toastStatus]);

  const onChangePools = useCallback(
    (newPools: PoolInfo[], options?: PoolChangeOptions) => {
      if (options?.isDelete) {
        skipSuccessToastRef.current = true;
      }
      setPools(newPools);
      debouncedSubmitPools(newPools);
    },
    [debouncedSubmitPools],
  );

  return (
    <MiningPools title="Pools" onChange={onChangePools} pools={pools} loading={poolsInfoPending && !isStalePools}>
      <DismissibleCalloutWrapper
        className={clsx({
          "mb-10!": createPoolsError?.error?.message !== undefined,
        })}
        icon={<Alert />}
        // TODO intent here has no effect, because callout doesn't have a header
        intent={intents.danger}
        show={createPoolsError?.error?.message !== undefined}
        title={createPoolsError?.error?.message ?? "An error occurred"}
        onDismiss={() => setCreatePoolsError(undefined)}
      />
    </MiningPools>
  );
};

export default SettingsMiningPools;
