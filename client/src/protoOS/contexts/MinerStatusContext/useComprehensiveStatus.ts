import { useMemo } from "react";
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
} from "./utility";
import {
  MiningStatusMiningstatus,
  NotificationError,
} from "@/protoOS/api/types";
import {
  isMining as checkIsMining,
  isSleeping as checkIsSleeping,
} from "@/protoOS/components/App/utility";
import { type StatusCircleProps } from "@/shared/components/StatusCircle";
import { statuses } from "@/shared/components/StatusCircle/constants";
import { createOrPredicate } from "@/shared/utils/predicate";

const useComprehensiveStatus = (
  errors: NotificationError[],
  miningStatus?: MiningStatusMiningstatus,
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

  const isSleeping = useMemo(() => {
    return checkIsSleeping(miningStatus?.status);
  }, [miningStatus]);

  const isMining = useMemo(() => {
    return checkIsMining(miningStatus?.status);
  }, [miningStatus]);

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
    } else if (errTitle) {
      return errTitle;
    } else {
      return {
        title: "All systems are operational",
        subtitle: undefined,
      };
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

export default useComprehensiveStatus;
