import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { removeToast } from "../../ToastsObserver";
import { type ToastType } from "../../types";
import ResizeablePanel from "@/protoOS/features/auth/components/LoginModal/ResizeablePanel";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import GroupedToast from "@/shared/features/toaster/components/GroupedToaster/GroupedToast";
import { defaultTtl, STATUSES } from "@/shared/features/toaster/constants";

interface GroupedToasterProps {
  toasts: ToastType[];
}

const GroupedToaster = ({ toasts }: GroupedToasterProps) => {
  const [isExpanded, setIsExpanded] = useState(false);
  const timeoutId = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hasAutoExpandedRef = useRef(false);

  const allCompleted = toasts.every((toast) => toast.status === STATUSES.success || toast.status === STATUSES.error);
  const hasErrors = toasts.some((toast) => toast.status === STATUSES.error);

  // Auto-expand once when:
  // 1. Errors are detected to draw user attention
  // 2. Multiple loading toasts appear (e.g., "taking longer than expected" message)
  useEffect(() => {
    const shouldAutoExpand =
      !isExpanded &&
      !hasAutoExpandedRef.current &&
      ((allCompleted && hasErrors) || (!allCompleted && toasts.length > 1));

    if (shouldAutoExpand) {
      hasAutoExpandedRef.current = true;
      queueMicrotask(() => setIsExpanded(true));
    }
    // Reset when all toasts are cleared (for next batch of toasts)
    if (toasts.length === 0) {
      hasAutoExpandedRef.current = false;
    }
  }, [allCompleted, hasErrors, isExpanded, toasts.length]);

  const handleToastClose = (id: number, customOnClose?: () => void) => {
    removeToast(id);
    customOnClose?.();
  };

  // When user doesn't expand the toaster, the toasts would never get removed
  // Skip auto-dismiss if there are errors so the user can see the error details
  useEffect(() => {
    const clearTimeoutWithCheck = () => {
      if (timeoutId.current !== null) {
        clearTimeout(timeoutId.current);
        timeoutId.current = null;
      }
    };

    const hasPersistentToast = toasts.some((t) => t.ttl === false);

    if (toasts.length === 0 || isExpanded || hasErrors || hasPersistentToast) {
      clearTimeoutWithCheck();
    }

    if (toasts.length !== 0 && !isExpanded && !hasErrors && !hasPersistentToast) {
      clearTimeoutWithCheck();

      timeoutId.current = setTimeout(
        () => {
          toasts.forEach((toast) => {
            if (toast.status !== STATUSES.success && toast.status !== STATUSES.error) return;

            handleToastClose(toast.id, toast.onClose);
          });
        },
        toasts[0].ttl !== false && toasts[0].ttl !== undefined ? toasts[0].ttl : defaultTtl,
      );
    }

    return clearTimeoutWithCheck;
  }, [toasts, isExpanded, hasErrors]);

  if (toasts.length === 0) return null;

  return (
    <div
      className={clsx(
        "relative w-100 overflow-hidden rounded-2xl bg-surface-elevated-base p-4 shadow-200 phone:w-[calc(100vw-theme(spacing.4))]",
        {
          "cursor-pointer": !isExpanded,
        },
      )}
      onClick={() => !isExpanded && setIsExpanded(true)}
      data-testid="toaster-container"
    >
      {(!isExpanded || toasts.length > 1) && (
        <div
          className={clsx("cursor-pointer", {
            "pb-2": isExpanded,
          })}
          onClick={() => setIsExpanded(!isExpanded)}
          data-testid="grouped-toaster-header"
        >
          <div className="flex items-center">
            <div className="flex flex-row items-center">
              <AnimatePresence initial={false} mode="popLayout">
                {!isExpanded && (
                  <motion.div
                    initial={{ x: "-100%", opacity: 0 }}
                    animate={{ x: 0, opacity: 1 }}
                    exit={{ x: "-100%", opacity: 0 }}
                    transition={{ duration: 0.3 }}
                    className="mr-3"
                  >
                    {allCompleted ? (
                      hasErrors ? (
                        <Alert className="text-intent-critical-fill" data-testid="header-error-icon" />
                      ) : (
                        <Success className="text-intent-success-fill" data-testid="header-success-icon" />
                      )
                    ) : (
                      <ProgressCircular indeterminate dataTestId="header-progress-circular" />
                    )}
                  </motion.div>
                )}
              </AnimatePresence>
              <motion.div layout transition={{ duration: 0.3 }}>
                <div className="flex flex-col">
                  <div className="text-emphasis-300 text-text-primary">
                    {allCompleted
                      ? toasts.length === 1
                        ? toasts[0].message
                        : `${toasts.length} updates complete`
                      : toasts.length === 1
                        ? toasts[0].message
                        : `${toasts.length} updates in progress`}
                  </div>
                </div>
              </motion.div>
            </div>
          </div>
        </div>
      )}
      <ResizeablePanel className="w-full" resizeOn={isExpanded ? toasts.length : false}>
        {isExpanded && (
          <>
            <div className="w-full divide-y divide-border-5">
              {toasts.map(({ message, status, id, progress, ttl, onClose, actions }) => (
                <GroupedToast
                  key={id}
                  message={message}
                  status={status}
                  progress={progress}
                  ttl={ttl}
                  actions={actions}
                  onClose={() => handleToastClose(id, onClose)}
                />
              ))}
            </div>
            <Button
              className="mt-2 w-full"
              variant={variants.secondary}
              text="Dismiss"
              onClick={() => {
                toasts.forEach((toast) => handleToastClose(toast.id, toast.onClose));
              }}
            />
          </>
        )}
      </ResizeablePanel>
    </div>
  );
};

export default GroupedToaster;
