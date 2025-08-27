import { useCallback, useEffect, useRef, useState } from "react";

import clsx from "clsx";
import { SimpleErrorProps } from "apiResponseTypes";
import { STATUS_MESSAGES } from "./constants";
import { useCreatePools } from "@/protoOS/api";

import MiningPools, {
  getEmptyPoolsInfo,
  isValidPool,
  PoolInfo,
} from "@/protoOS/components/MiningPools";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
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
  const [toastStatus, setToastStatus] = useState<ToastStatusType | null>(null);
  const [isStalePools, setIsStalePools] = useState(false);
  const toastId = useRef<number | null>(null);

  const { poolsInfo, poolsInfoStatus } = useMinerStatus();
  const { createPools } = useCreatePools();
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
    }
  }, [poolsInfo]);

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const debouncedSubmitPools = useCallback(
    debounce((newPools: PoolInfo[]) => {
      setToastStatus(TOAST_STATUSES.loading);
      removeToast(toastId.current);
      toastId.current = pushToast({
        message: STATUS_MESSAGES.loading,
        status: TOAST_STATUSES.loading,
      });

      const validPools = newPools.filter(isValidPool);
      createPools({
        poolInfo: validPools,
        onSuccess: () => {
          setCreatePoolsError(undefined);
          setIsStalePools(true);
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
    }),
    [createPools],
  );

  useEffect(() => {
    if (
      toastStatus === TOAST_STATUSES.loading &&
      isStalePools &&
      !poolsInfoStatus.pending
    ) {
      if (
        poolsInfoStatus.error &&
        !/failed to connect to cgminer/i.test(poolsInfoStatus.error)
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
  }, [isStalePools, poolsInfo, poolsInfoStatus, toastStatus]);

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
      loading={poolsInfoStatus.pending && !isStalePools}
    >
      <DismissibleCalloutWrapper
        className={clsx({
          "mb-10!": createPoolsError?.error !== undefined,
        })}
        icon={<Alert />}
        // TODO intent here has no effect, because callout doesn't have a header
        intent={intents.danger}
        show={createPoolsError?.error !== undefined}
        title={createPoolsError?.error}
        onDismiss={() => setCreatePoolsError(undefined)}
      />
    </MiningPools>
  );
};

export default SettingsMiningPools;
