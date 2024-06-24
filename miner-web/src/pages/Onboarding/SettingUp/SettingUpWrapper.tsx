import { useCallback, useEffect, useMemo, useState } from "react";
import { createSearchParams, useNavigate } from "react-router-dom";

import { useCreatePool, usePoolsInfo } from "api";

import { isValidPool, PoolInfo } from "components/MiningPools";

import { statuses } from "./constants";
import SettingUp from "./SettingUp";

interface SettingUpWrapperProps {
  pools: PoolInfo[];
}

const SettingUpWrapper = ({ pools }: SettingUpWrapperProps) => {
  const navigate = useNavigate();
  const { createPool } = useCreatePool();
  const { fetch: fetchPools } = usePoolsInfo();
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [poolStatus, setPoolStatus] = useState<keyof typeof statuses>(
    statuses.fetch
  );

  const getPoolStatus = useCallback(() => {
    fetchPools({
      onSuccess: () => setPoolStatus(statuses.success),
      onError: (error = "") => {
        // wait for cgminer to restart before marking pools as configured
        const message = error.toLowerCase?.() || error;
        if (!/failed to connect to cgminer/.test(message)) {
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
      setPoolStatus(statuses.pending);
      const validPools = pools.filter(isValidPool);
      createPool({
        poolInfo: validPools,
        onSuccess: () => {
          const newIntervalId = setInterval(getPoolStatus, 2500);
          setIntervalId(newIntervalId);
        },
        onError: () => setPoolStatus(statuses.error),
      });
    }
  }, [createPool, getPoolStatus, poolStatus, pools]);

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

  const handleClickContinue = useCallback(
    () =>
      navigate({
        pathname: "/",
        search: `?${createSearchParams({ onboarding: "true" })}`,
      }),
    [navigate]
  );

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
