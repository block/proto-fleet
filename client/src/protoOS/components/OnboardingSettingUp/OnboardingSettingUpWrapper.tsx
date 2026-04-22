import { useCallback, useEffect, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useCreatePools } from "@/protoOS/api/hooks/useCreatePools";
import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";
import { useSystemStatus } from "@/protoOS/api/hooks/useSystemStatus";

import { isValidPool, PoolInfo } from "@/protoOS/components/MiningPools";
import OnboardingSettingUp from "@/shared/components/OnboardingSettingUp/OnboardingSettingUp";
import { statuses } from "@/shared/constants/statuses";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface OnboardingSettingUpWrapperProps {
  onChangeSettingUpMiner: (settingUpMiner: boolean) => void;
  pools: PoolInfo[];
  setCreatePoolsError: (error: ErrorProps) => void;
}

const OnboardingSettingUpWrapper = ({
  onChangeSettingUpMiner,
  pools,
  setCreatePoolsError,
}: OnboardingSettingUpWrapperProps) => {
  const navigate = useNavigate();
  const { createPools } = useCreatePools();
  const { fetchData: fetchPools } = usePoolsInfo();
  const { reload: reloadSystemStatus } = useSystemStatus();
  const [intervalId, setIntervalId] = useState<ReturnType<typeof setInterval>>();
  const [poolStatus, setPoolStatus] = useState<keyof typeof statuses>(statuses.fetch);

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

  const isConfigured = useCallback((status: keyof typeof statuses) => status === statuses.success, []);

  const handleClickRetry = useCallback(() => {
    setPoolStatus(statuses.fetch);
  }, []);

  const isSetupDone = useMemo(() => isConfigured(poolStatus), [isConfigured, poolStatus]);

  const handleClickContinue = useCallback(() => {
    // Refresh system status to get updated onboarded flag from API
    // This will update the store, which will prevent redirect loops
    reloadSystemStatus();
    // Navigate to home
    navigate("/");
  }, [navigate, reloadSystemStatus]);

  const handleClickReconfigure = useCallback(() => onChangeSettingUpMiner(false), [onChangeSettingUpMiner]);

  return (
    <OnboardingSettingUp
      poolStatus={poolStatus}
      isSetupDone={isSetupDone}
      onClickContinue={handleClickContinue}
      onClickReconfigure={handleClickReconfigure}
      onClickRetry={handleClickRetry}
    />
  );
};

export default OnboardingSettingUpWrapper;
