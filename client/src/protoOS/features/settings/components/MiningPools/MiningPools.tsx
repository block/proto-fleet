import { useCallback, useEffect, useRef, useState } from "react";

import clsx from "clsx";
import { SimpleErrorProps } from "apiResponseTypes";
import { STATUS_MESSAGES } from "./constants";
import { useCreatePools, useEditPool, usePoolsInfo } from "@/protoOS/api";

import MiningPools, {
  getEmptyPoolsInfo,
  isValidPool,
  PoolInfo,
} from "@/protoOS/components/MiningPools";
import { Alert } from "@/shared/assets/icons";
import {
  DismissibleCalloutWrapper,
  intents,
} from "@/shared/components/Callout";
import {
  pushToast,
  removeToast,
  STATUSES as TOAST_STATUSES,
  ToastStatusType,
} from "@/shared/features/toaster";
import { debounce } from "@/shared/utils/utility";

const SettingsMiningPools = () => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [previousPools, setPreviousPools] =
    useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [toastStatus, setToastStatus] = useState<ToastStatusType | null>(null);
  const [isStalePools, setIsStalePools] = useState(false);
  const toastId = useRef<number | null>(null);

  const {
    data: poolsInfo,
    pending: poolsInfoPending,
    error: poolsInfoError,
  } = usePoolsInfo();
  const { createPools } = useCreatePools();
  const { editPool } = useEditPool();
  const [createPoolsError, setCreatePoolsError] = useState<SimpleErrorProps>();

  useEffect(() => {
    if (poolsInfo?.length) {
      const newPools = [...Array(3)].map((_, index) => ({
        url: poolsInfo?.[index]?.url || "",
        username: poolsInfo?.[index]?.user || "",
        password: "",
        priority: poolsInfo[index]?.priority || index,
      }));
      setPools(newPools);
      setPreviousPools(newPools);
    }
  }, [poolsInfo]);

  const findChangedPool = useCallback(
    (currentPools: PoolInfo[], previousPools: PoolInfo[]) => {
      let changedIndex = -1;
      let changesCount = 0;

      for (let i = 0; i < currentPools.length; i++) {
        const current = currentPools[i];
        const previous = previousPools[i];

        if (
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
    },
    [],
  );

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const debouncedSubmitPools = useCallback(
    debounce((newPools: PoolInfo[]) => {
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
    }),
    [createPools, editPool, findChangedPool, previousPools, poolsInfo],
  );

  useEffect(() => {
    if (
      toastStatus === TOAST_STATUSES.loading &&
      isStalePools &&
      !poolsInfoPending
    ) {
      if (
        poolsInfoError &&
        !/failed to connect to cgminer/i.test(poolsInfoError)
      ) {
        setToastStatus(TOAST_STATUSES.error);
        removeToast(toastId.current);
        toastId.current = pushToast({
          message: STATUS_MESSAGES.error,
          status: TOAST_STATUSES.error,
        });

        setIsStalePools(false);
      } else if (poolsInfo?.length) {
        setToastStatus(TOAST_STATUSES.success);
        removeToast(toastId.current);
        toastId.current = pushToast({
          message: STATUS_MESSAGES.success,
          status: TOAST_STATUSES.success,
        });
        setIsStalePools(false);
      }
    }
  }, [isStalePools, poolsInfo, poolsInfoPending, poolsInfoError, toastStatus]);

  const onChangePools = useCallback(
    (newPools: PoolInfo[]) => {
      setPools(newPools);
      debouncedSubmitPools(newPools);
    },
    [debouncedSubmitPools],
  );

  return (
    <MiningPools
      title="Mining Pools"
      onChange={onChangePools}
      pools={pools}
      loading={poolsInfoPending && !isStalePools}
    >
      <DismissibleCalloutWrapper
        className={clsx({
          "mb-10!": createPoolsError?.error !== undefined,
        })}
        icon={<Alert />}
        // TODO intent here has no effect, because callout doesn't have a header
        intent={intents.danger}
        show={createPoolsError?.error !== undefined}
        title={createPoolsError?.error ?? "An error occurred"}
        onDismiss={() => setCreatePoolsError(undefined)}
      />
    </MiningPools>
  );
};

export default SettingsMiningPools;
