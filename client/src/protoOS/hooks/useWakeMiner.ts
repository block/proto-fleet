import { useCallback, useEffect, useRef, useState } from "react";

import { useMiningStart, useMiningStatus } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { MiningStatusMiningstatus } from "@/protoOS/api/types";
import { isSleeping } from "@/protoOS/components/App/utility";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import {
  useAccessToken,
  useAuthContext,
} from "@/protoOS/features/auth/contexts/AuthContext";

interface UseWakeMinerProps {
  afterWake?: () => void;
  miningStatus?: MiningStatusMiningstatus;
  onSuccess?: () => void;
  onError?: (error: ErrorProps) => void;
}

export const useWakeMiner = ({
  afterWake,
  miningStatus,
  onSuccess,
  onError,
}: UseWakeMinerProps = {}) => {
  const { startMining } = useMiningStart();
  const { fetchData: fetchMiningStatus } = useMiningStatus({ poll: false });
  const { setMiningStatus } = useMinerStatus();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<ErrorProps>();
  const [warnWake, setWarnWake] = useState(false);
  const [shouldWake, setShouldWake] = useState(false);
  const [pausedAction, setPausedAction] = useState(false);
  const afterWakeRef = useRef(afterWake);
  const onSuccessRef = useRef(onSuccess);
  const onErrorRef = useRef(onError);
  const isWakingRef = useRef(false);
  const intervalIdRef = useRef<ReturnType<typeof setInterval>>();

  afterWakeRef.current = afterWake;
  onSuccessRef.current = onSuccess;
  onErrorRef.current = onError;

  const pollMiningStatus = useCallback(() => {
    if (intervalIdRef.current) {
      clearInterval(intervalIdRef.current);
    }

    const newIntervalId = setInterval(() => {
      fetchMiningStatus({
        onSuccess: (newMiningStatus) => {
          setMiningStatus(newMiningStatus);
          if (newMiningStatus && !isSleeping(newMiningStatus.status)) {
            // Miner is awake - stop polling and reset state
            clearInterval(newIntervalId);
            intervalIdRef.current = undefined;
            setShouldWake(false);
            setPending(false);
            isWakingRef.current = false;
            afterWakeRef.current?.();
            onSuccessRef.current?.();
          }
        },
      });
    }, 2000);

    intervalIdRef.current = newIntervalId;
  }, [fetchMiningStatus, setMiningStatus]);

  const { dismissedLoginModal, setDismissedLoginModal } = useAuthContext();
  const { checkAccess, hasAccess } = useAccessToken(
    pausedAction && !dismissedLoginModal,
  );

  useEffect(() => {
    if (hasAccess && pausedAction) {
      setPausedAction(false);
      setWarnWake(true);
    }
  }, [hasAccess, pausedAction]);

  // Handle dismissed login modal
  useEffect(() => {
    if (dismissedLoginModal) {
      setPausedAction(false);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setDismissedLoginModal]);

  useEffect(() => {
    if (
      miningStatus &&
      !isSleeping(miningStatus.status) &&
      isWakingRef.current
    ) {
      setShouldWake(false);
      setPending(false);
      isWakingRef.current = false;
      afterWakeRef.current?.();
      onSuccessRef.current?.();
    }
  }, [miningStatus]);

  const executeWake = useCallback(() => {
    setError(undefined);
    setShouldWake(true);
    setPending(true);
    isWakingRef.current = true;

    startMining({
      onSuccess: () => {
        // Start polling to detect when miner wakes up
        pollMiningStatus();

        // Add a fallback timeout in case polling doesn't work
        setTimeout(() => {
          if (intervalIdRef.current) {
            clearInterval(intervalIdRef.current);
            intervalIdRef.current = undefined;
          }
          setPending(false);
          setShouldWake(false);
          isWakingRef.current = false;
        }, 15000); // 15 second fallback
      },
      onError: (err) => {
        setError(err);
        setShouldWake(false);
        setPending(false);
        isWakingRef.current = false;
        onErrorRef.current?.(err);
      },
    });
  }, [startMining, pollMiningStatus]);

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (intervalIdRef.current) {
        clearInterval(intervalIdRef.current);
      }
    };
  }, []);

  const handleWakeConfirm = useCallback(() => {
    setWarnWake(false);
    executeWake();
  }, [executeWake]);

  const wakeMiner = useCallback(() => {
    setPausedAction(true);
    checkAccess();
  }, [checkAccess]);

  return {
    wakeMiner,
    pending,
    error,
    clearError: () => setError(undefined),
    warnWake,
    shouldWake,
    handleWakeConfirm,
    onWarnWakeClose: () => setWarnWake(false),
  };
};
