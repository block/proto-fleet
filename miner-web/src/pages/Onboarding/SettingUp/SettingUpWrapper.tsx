import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { useCoolingMode, useCreatePool, usePoolsInfo } from "api";

import { FanMode } from "components/Cooling";
import { isValidPool, PoolInfo } from "components/MiningPools";

import { statuses } from "./constants";
import SettingUp from "./SettingUp";

interface SettingUpWrapperProps {
  fanMode: FanMode;
  pools: PoolInfo[];
}

const SettingUpWrapper = ({ fanMode, pools }: SettingUpWrapperProps) => {
  const navigate = useNavigate();
  const { createPool } = useCreatePool();
  const { setCoolingMode } = useCoolingMode();
  const { fetch: fetchPools } = usePoolsInfo();
  const [intervalId, setIntervalId] =
    useState<ReturnType<typeof setInterval>>();
  const [poolStatus, setPoolStatus] = useState<keyof typeof statuses>(
    statuses.fetch
  );
  const [fanStatus, setFanStatus] = useState<keyof typeof statuses>(
    statuses.fetch
  );

  const getPoolStatus = useCallback(() => {
    fetchPools({
      onSuccess: () => setPoolStatus(statuses.success),
      onError: (error) => {
        // wait for cgminer to restart before marking pools as configured
        const message = (error?.message || "").toLowerCase();
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

  useEffect(() => {
    if (fanStatus === statuses.fetch) {
      setFanStatus(statuses.pending);
      setCoolingMode({
        fanMode,
        onSuccess: () => setFanStatus(statuses.success),
        onError: () => setFanStatus(statuses.error),
      });
    }
  }, [fanMode, fanStatus, setCoolingMode]);

  const isConfigured = useCallback(
    (status: keyof typeof statuses) =>
      status === statuses.success || status === statuses.error,
    []
  );

  const handleClickRetry = useCallback(() => {
    setPoolStatus(statuses.fetch);
  }, []);

  const isSetupDone = useMemo(
    () => isConfigured(poolStatus) && isConfigured(fanStatus),
    [fanStatus, isConfigured, poolStatus]
  );

  const handleClickContinue = useCallback(() => navigate("/"), [navigate]);

  return (
    <SettingUp
      fanStatus={fanStatus}
      poolStatus={poolStatus}
      isSetupDone={isSetupDone}
      onClickContinue={handleClickContinue}
      onClickRetry={handleClickRetry}
    />
  );
};

export default SettingUpWrapper;
