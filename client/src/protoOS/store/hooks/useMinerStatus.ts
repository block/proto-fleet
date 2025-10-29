import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import useMinerStore from "../useMinerStore";
import {
  getErrorMessage,
  getErrorTitle,
  getStatusErrorTitle,
  getStatusSummary,
  isAsicError,
  isAsicWarning,
  isControlBoardError,
  isControlBoardWarning,
  isFanError,
  isFanWarning,
  isHashboardError,
  isHashboardWarning,
  isPSUError,
  isPSUWarning,
} from "../utils/errorUtils";
import { NotificationError } from "@/protoOS/api/generatedApi";
import { type StatusCircleProps } from "@/shared/components/StatusCircle";
import { statuses } from "@/shared/components/StatusCircle/constants";
import { createOrPredicate } from "@/shared/utils/predicate";

// =============================================================================
// Comprehensive Status Calculation
// this combines errors and mining status to create a comprehensive status object
// that is the type expected by our shared components (ie MinerStatusModal.status)
// =============================================================================

const useComprehensiveStatusCalc = (
  errors: NotificationError[],
  isSleeping: boolean,
  isMining: boolean,
) => {
  const hashboardIssues = useMemo(
    () =>
      errors?.filter(
        createOrPredicate<NotificationError>(
          isHashboardError,
          isAsicError,
          isHashboardWarning,
          isAsicWarning,
        ),
      ) || [],
    [errors],
  );

  const psuIssues = useMemo(
    () =>
      errors?.filter(
        createOrPredicate<NotificationError>(isPSUError, isPSUWarning),
      ) || [],
    [errors],
  );

  const fanIssues = useMemo(
    () =>
      errors?.filter(
        createOrPredicate<NotificationError>(isFanError, isFanWarning),
      ) || [],
    [errors],
  );

  const controlBoardIssues = useMemo(
    () =>
      errors?.filter(
        createOrPredicate<NotificationError>(
          isControlBoardError,
          isControlBoardWarning,
        ),
      ) || [],
    [errors],
  );

  // determine messaging based on issue type and count
  const issueStatusText = useMemo(() => {
    return getStatusSummary(
      hashboardIssues,
      psuIssues,
      fanIssues,
      controlBoardIssues,
    );
  }, [hashboardIssues, psuIssues, fanIssues, controlBoardIssues]);

  /**
   * Determines the status summary based on priority of various miningStatuses and error states
   * This is similar to the title but with less detail
   *
   * priority:
   * 1. MiningStatus.status === "Stopped" -> sleeping
   * 2. Issues -> issue status
   * 3. MiningStatus.status === "Mining" | "DegradedMining" -> hashing
   *
   */
  const summary = useMemo(() => {
    if (isSleeping) {
      return "Sleeping";
    } else if (issueStatusText) {
      return issueStatusText;
    } else if (isMining) {
      return "Hashing";
    }
  }, [issueStatusText, isSleeping, isMining]);

  const { title, subtitle } = useMemo(() => {
    const errTitle = getStatusErrorTitle(errors);
    if (isSleeping) {
      return {
        title: "Miner is asleep",
        subtitle: undefined,
      };
    } else {
      return errTitle;
    }
  }, [errors, isSleeping]);

  const normalizeIssueDetails = (issue: NotificationError) => {
    const title = getErrorTitle(issue);
    const message = getErrorMessage(issue);
    const details = issue.details;
    return {
      title,
      message,
      details,
    };
  };

  const issues = useMemo(() => {
    return {
      fans: fanIssues.map(normalizeIssueDetails),
      psus: psuIssues.map(normalizeIssueDetails),
      hashboards: hashboardIssues.map(normalizeIssueDetails),
      controlBoard: controlBoardIssues.map(normalizeIssueDetails),
    };
  }, [fanIssues, psuIssues, hashboardIssues, controlBoardIssues]);

  const hasIssues = useMemo(() => {
    return Object.values(issues).some((issueList) => issueList.length > 0);
  }, [issues]);

  const circle = useMemo<StatusCircleProps["status"]>(() => {
    if (isSleeping) {
      return statuses.sleeping;
    }

    if (
      errors.some(
        createOrPredicate<NotificationError>(
          isFanError,
          isControlBoardError,
          isHashboardError,
          isAsicError,
          isPSUError,
          isFanWarning,
          isControlBoardWarning,
          isHashboardWarning,
          isAsicWarning,
          isPSUWarning,
        ),
      )
    ) {
      return statuses.error;
    }

    return statuses.normal;
  }, [errors, isSleeping]);

  return useMemo(() => {
    return {
      isSleeping,
      isMining,
      summary,
      circle,
      title,
      subtitle,
      hasIssues,
      issues,
    };
  }, [
    summary,
    circle,
    title,
    subtitle,
    issues,
    hasIssues,
    isSleeping,
    isMining,
  ]);
};

// =============================================================================
// Granular Hooks for Specific Data
// =============================================================================

export const useMiningStatus = () => {
  return useMinerStore((state) => state.minerStatus.miningStatus);
};

export const useMiningUptime = () => {
  return useMinerStore((state) => state.minerStatus.miningUptime);
};

export const useRebootUptime = () => {
  return useMinerStore((state) => state.minerStatus.rebootUptime);
};

export const useHwErrors = () => {
  return useMinerStore((state) => state.minerStatus.hwErrors);
};

export const useMiningStatusMessage = () => {
  return useMinerStore((state) => state.minerStatus.message);
};

// Derived flag hooks - compute from state
export const useIsWarmingUp = () => {
  return useMinerStore((state) => {
    const status = state.minerStatus.miningStatus || "";
    const miningUptimeS = state.minerStatus.miningUptime?.value || 0;
    const rebootUptimeS = state.minerStatus.rebootUptime?.value || 0;

    return (
      /Uninitialized|PoweringOn|NoPools/i.test(status) &&
      (miningUptimeS < 60 || rebootUptimeS < 60)
    );
  });
};

export const useIsSleeping = () => {
  return useMinerStore((state) => {
    const status = state.minerStatus.miningStatus || "";
    return /PoweringOff|Stopped/i.test(status);
  });
};

export const useIsMining = () => {
  return useMinerStore((state) => {
    const status = state.minerStatus.miningStatus || "";
    return /Mining/i.test(status);
  });
};

export const useIsAwake = () => {
  return useMinerStore((state) => {
    const status = state.minerStatus.miningStatus || "";
    return /PoweringOn|Mining|DegradedMining|NoPools|Error/i.test(status);
  });
};

export const useMinerErrors = () => {
  return useMinerStore(useShallow((state) => state.minerStatus.errors));
};

export const usePoolsInfo = () => {
  return useMinerStore(
    useShallow((state) => ({
      poolsInfo: state.minerStatus.poolsInfo,
      poolsInfoStatus: state.minerStatus.poolsInfoStatus,
    })),
  );
};

export const useWakeDialog = () => {
  return useMinerStore(useShallow((state) => state.ui.wakeDialog));
};

/**
 * Hook to get the comprehensive status computed from errors and mining status
 * This hook basically combines useMinerErrors and useMiningStatus to creates a status object
 * with the type that our shared components expect (ie MinerStatusModal.status)
 */
export const useComprehensiveStatus = () => {
  const errors = useMinerStore(
    useShallow((state) => state.minerStatus.errors.errors),
  );
  const isSleeping = useIsSleeping();
  const isMining = useIsMining();

  return useComprehensiveStatusCalc(errors ?? [], isSleeping, isMining);
};

// =============================================================================
// Action Hooks
// =============================================================================

export const useSetMiningStatus = () => {
  return useMinerStore((state) => state.minerStatus.setMiningStatus);
};

export const useSetErrors = () => {
  return useMinerStore((state) => state.minerStatus.setErrors);
};

export const useSetPoolsInfo = () => {
  return useMinerStore((state) => state.minerStatus.setPoolsInfo);
};

export const useShowWakeDialog = () => {
  return useMinerStore((state) => state.ui.showWakeDialog);
};

export const useHideWakeDialog = () => {
  return useMinerStore((state) => state.ui.hideWakeDialog);
};
