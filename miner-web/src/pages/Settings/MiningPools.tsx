import { useCallback, useEffect, useRef, useState } from "react";

import { useCreatePools } from "api";

import { useAccessToken } from "common/hooks/useAccessToken";
import { useApiContext } from "common/hooks/useApiContext";
import { debounce } from "common/utils/utility";

import MiningPools, {
  getEmptyPoolsInfo,
  isValidPool,
  PoolInfo,
} from "components/MiningPools";

import { 
  pushToast, 
  removeToast, 
  STATUSES as TOAST_STATUSES, 
  ToastStatusType
} from "components/Toaster";

import { STATUS_MESSAGES } from "./constants";

const SettingsMiningPools = () => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [toastStatus, setToastStatus] = useState<ToastStatusType | null>(null);
  const [isStalePools, setIsStalePools] = useState(false);
  const toastId = useRef<number | null>(null)

  const { poolsInfo, poolsInfoStatus } = useApiContext();
  const { createPools } = useCreatePools();
  
  useAccessToken();

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
          setIsStalePools(true);
        },
        onError: () => {
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
    [createPools]
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
    [debouncedSubmitPools]
  );

  return (
    <MiningPools
      onChange={onChangePools}
      pools={pools}
      loading={poolsInfoStatus.pending && !isStalePools}
    />
  );
};

export default SettingsMiningPools;
