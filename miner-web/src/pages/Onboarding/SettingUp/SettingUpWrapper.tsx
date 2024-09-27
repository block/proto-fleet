import { useCallback, useEffect, useMemo, useState } from "react";

import { useCreatePools, usePoolsInfo } from "api";
import { ErrorProps } from "apiResponseTypes";

import { useNavigate } from "common/hooks/useNavigate";

import { isValidPool, PoolInfo } from "components/MiningPools";

import { statuses } from "./constants";
import SettingUp from "./SettingUp";

interface SettingUpWrapperProps {
  pools: PoolInfo[];
  setCreatePoolsError: (error: ErrorProps) => void;
}

const SettingUpWrapper = ({
  pools,
  setCreatePoolsError,
}: SettingUpWrapperProps) => {
  const navigate = useNavigate();
  const { createPools } = useCreatePools();
  const { fetchData: fetchPools } = usePoolsInfo();
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [poolStatus, setPoolStatus] = useState<keyof typeof statuses>(
    statuses.fetch
  );

  const getPoolStatus = useCallback(() => {
    fetchPools({
      onSuccess: () => setPoolStatus(statuses.success),
      onError: (message = "") => {
        // wait for cgminer to restart before marking pools as configured
        if (!/failed to connect to cgminer/i.test(message)) {
          setPoolStatus(statuses.error);
        }
      },
    });
  }, [fetchPools]);

  useEffect(() => {
    if (poolStatus !== statuses.pending && intervalId) {
      clearInterval(intervalId);
      setIntervalId(undefined);
    }
  }, [intervalId, poolStatus]);

  useEffect(() => {
    if (poolStatus === statuses.fetch) {
      setCreatePoolsError(undefined);
      setPoolStatus(statuses.pending);
      const validPools = pools.filter(isValidPool);
      createPools({
        poolInfo: validPools,
        onSuccess: () => {
          const newIntervalId = setInterval(getPoolStatus, 2500);
          setIntervalId(newIntervalId);
        },
        onError: (error) => {
          setCreatePoolsError(error);
          setPoolStatus(statuses.error);
        },
      });
    }
  }, [createPools, getPoolStatus, poolStatus, pools, setCreatePoolsError]);

  const isConfigured = useCallback(
    (status: keyof typeof statuses) =>
      status === statuses.success || status === statuses.error,
    []
  );

  const handleClickRetry = useCallback(() => {
    setPoolStatus(statuses.fetch);
  }, []);

  const isSetupDone = useMemo(
    () => isConfigured(poolStatus),
    [isConfigured, poolStatus]
  );

  const handleClickContinue = useCallback(() => navigate("/"), [navigate]);

  return (
    <SettingUp
      poolStatus={poolStatus}
      isSetupDone={isSetupDone}
      onClickContinue={handleClickContinue}
      onClickRetry={handleClickRetry}
    />
  );
};

export default SettingUpWrapper;
