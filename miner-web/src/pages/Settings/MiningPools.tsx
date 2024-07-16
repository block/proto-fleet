import { useCallback, useEffect, useState } from "react";

import { useCreatePools } from "api";

import { useApiContext } from "common/hooks/useApiContext";
import { debounce } from "common/utils/utility";

import MiningPools, {
  getEmptyPoolsInfo,
  isValidPool,
  PoolInfo,
} from "components/MiningPools";
import { ToastType, toastTypes } from "components/Toast";

import StatusToast from "./StatusToast";

const SettingsMiningPools = () => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [toastType, setToastType] = useState<ToastType | null>(null);
  const [isStalePools, setIsStalePools] = useState(false);

  const { poolsInfo, poolsInfoStatus } = useApiContext();
  const { createPools } = useCreatePools();

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
      setToastType(toastTypes.loading);
      const validPools = newPools.filter(isValidPool);
      createPools({
        poolInfo: validPools,
        onSuccess: () => {
          setIsStalePools(true);
        },
        onError: () => {
          setToastType(toastTypes.error);
        },
        retryOnMinerDown: true,
      });
    }),
    [createPools]
  );

  useEffect(() => {
    if (
      toastType === toastTypes.loading &&
      isStalePools &&
      !poolsInfoStatus.pending
    ) {
      if (
        poolsInfoStatus.error &&
        !/failed to connect to cgminer/i.test(poolsInfoStatus.error)
      ) {
        setToastType(toastTypes.error);
        setIsStalePools(false);
      } else if (poolsInfo?.length) {
        setToastType(toastTypes.success);
        setIsStalePools(false);
      }
    }
  }, [isStalePools, poolsInfo, poolsInfoStatus, toastType]);

  const onChangePools = useCallback(
    (newPools: PoolInfo[]) => {
      setPools(newPools);
      debouncedSubmitPools(newPools);
    },
    [debouncedSubmitPools]
  );

  return (
    <>
      <StatusToast onClose={() => setToastType(null)} type={toastType} />
      <MiningPools
        onChange={onChangePools}
        pools={pools}
        loading={poolsInfoStatus.pending && !isStalePools}
      />
    </>
  );
};

export default SettingsMiningPools;
