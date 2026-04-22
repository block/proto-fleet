import { useCallback, useEffect, useRef, useState } from "react";
import { useMiningStart, useMiningStatus } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useAccessToken } from "@/protoOS/store";
import {
  AUTH_ACTIONS,
  useDismissedLoginModal,
  useHideWakeDialog,
  useIsSleeping,
  usePausedAuthAction,
  useSetDismissedLoginModal,
  useSetMiningStatus,
  useSetPausedAuthAction,
  useShowWakeDialog,
} from "@/protoOS/store";

interface UseWakeMinerProps {
  afterWake?: () => void;
  onSuccess?: () => void;
  onError?: (error: ErrorProps) => void;
}

export const useWakeMiner = ({ afterWake, onSuccess, onError }: UseWakeMinerProps = {}) => {
  const { startMining } = useMiningStart();
  const { fetchData: fetchMiningStatus } = useMiningStatus({ poll: false });
  const setMiningStatus = useSetMiningStatus();
  const showWakeDialog = useShowWakeDialog();
  const hideWakeDialog = useHideWakeDialog();
  const isSleeping = useIsSleeping();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<ErrorProps>();
  const [shouldWake, setShouldWake] = useState(false);
  const dismissedLoginModal = useDismissedLoginModal();
  const setDismissedLoginModal = useSetDismissedLoginModal();
  const pausedAuthAction = usePausedAuthAction();
  const setPausedAuthAction = useSetPausedAuthAction();
  const { checkAccess, hasAccess } = useAccessToken(!!pausedAuthAction && !dismissedLoginModal);
  const afterWakeRef = useRef(afterWake);
  const onSuccessRef = useRef(onSuccess);
  const onErrorRef = useRef(onError);
  const isWakingRef = useRef(false);
  const intervalIdRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined);

  useEffect(() => {
    afterWakeRef.current = afterWake;
    onSuccessRef.current = onSuccess;
    onErrorRef.current = onError;
  }, [afterWake, onSuccess, onError]);

  const pollMiningStatus = useCallback(() => {
    if (intervalIdRef.current) {
      clearInterval(intervalIdRef.current);
    }

    const newIntervalId = setInterval(() => {
      fetchMiningStatus({
        onSuccess: (newMiningStatus) => {
          setMiningStatus(newMiningStatus);
        },
      });
    }, 2000);

    intervalIdRef.current = newIntervalId;
  }, [fetchMiningStatus, setMiningStatus]);

  // Handle dismissed login modal
  useEffect(() => {
    if (dismissedLoginModal) {
      setPausedAuthAction(null);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setDismissedLoginModal, setPausedAuthAction]);

  useEffect(() => {
    if (!isSleeping && isWakingRef.current) {
      // Miner is awake - stop polling and reset state
      if (intervalIdRef.current) {
        clearInterval(intervalIdRef.current);
        intervalIdRef.current = undefined;
      }
      isWakingRef.current = false;

      setShouldWake(false);
      setPending(false);
      afterWakeRef.current?.();
      onSuccessRef.current?.();
    }
  }, [isSleeping]);

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
    hideWakeDialog();
    executeWake();
  }, [executeWake, hideWakeDialog]);

  useEffect(() => {
    if (hasAccess && pausedAuthAction) {
      if (pausedAuthAction === AUTH_ACTIONS.wake) {
        showWakeDialog(handleWakeConfirm, () => hideWakeDialog());
      }
      setPausedAuthAction(null);
    }
  }, [hasAccess, pausedAuthAction, setPausedAuthAction, showWakeDialog, hideWakeDialog, handleWakeConfirm]);

  const wakeMiner = useCallback(() => {
    setPausedAuthAction(AUTH_ACTIONS.wake);
    checkAccess();
  }, [checkAccess, setPausedAuthAction]);

  return {
    wakeMiner,
    pending,
    error,
    clearError: () => setError(undefined),
    shouldWake,
  };
};
