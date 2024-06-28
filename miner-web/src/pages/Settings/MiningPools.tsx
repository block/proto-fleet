import { useCallback, useContext, useEffect, useState } from "react";

import { ApiContext, useCreatePools } from "api";

import { debounce, deepClone } from "common/utils/utility";

import MiningPools, {
  emptyPoolInfo,
  isValidPool,
  PoolInfo,
} from "components/MiningPools";
import { ToastType, toastTypes } from "components/Toast";

import StatusToast from "./StatusToast";

const SettingsMiningPools = () => {
  // pools is an array of 3 PoolInfo objects
  // index 0 is the default pool, then backups 1 and 2
  // [{url: "", username: "", password: ""}, x3]
  const [pools, setPools] = useState<PoolInfo[]>(
    Array(3).fill(deepClone(emptyPoolInfo))
  );
  const [toastType, setToastType] = useState<ToastType | null>(null);

  const { poolsInfo } = useContext(ApiContext);
  const { createPools } = useCreatePools();

  useEffect(() => {
    if (poolsInfo.length && !pools[0].url) {
      const newPools = [...Array(3)].map((_, index) => ({
        url: poolsInfo[index]?.url || "",
        username: poolsInfo[index]?.user || "",
        password: "",
      }));
      setPools(newPools);
    }
  }, [poolsInfo, pools]);

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const debouncedSubmitPools = useCallback(
    debounce((newPools: PoolInfo[]) => {
      setToastType(toastTypes.loading);
      const validPools = newPools.filter(isValidPool);
      createPools({
        poolInfo: validPools,
        onSuccess: () => {
          setToastType(toastTypes.success);
        },
        onError: () => {
          setToastType(toastTypes.error);
        },
      });
    }),
    [createPools]
  );

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
      <MiningPools onChange={onChangePools} pools={pools} />
    </>
  );
};

export default SettingsMiningPools;
