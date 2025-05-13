import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { removeToast } from "../../ToastsObserver";
import { type ToastType } from "../../types";
import ResizeablePanel from "@/protoOS/features/auth/components/LoginModal/ResizeablePanel";
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

  // When user doesn't expand the toaster, the toasts would never get removed
  useEffect(() => {
    const clearTimeoutWithCheck = () => {
      if (timeoutId.current !== null) {
        clearTimeout(timeoutId.current);
        timeoutId.current = null;
      }
    };

    if (toasts.length === 0 || isExpanded) {
      clearTimeoutWithCheck();
    }

    if (toasts.length !== 0 && !isExpanded) {
      clearTimeoutWithCheck();

      timeoutId.current = setTimeout(
        () => {
          toasts.forEach((toast) => {
            if (
              toast.status !== STATUSES.success &&
              toast.status !== STATUSES.error
            )
              return;

            removeToast(toast.id);
          });
        },
        toasts[0].ttl !== false && toasts[0].ttl !== undefined
          ? toasts[0].ttl
          : defaultTtl,
      );
    }

    return clearTimeoutWithCheck;
  }, [toasts, isExpanded]);

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
    >
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
                >
                  <ProgressCircular
                    key="progress"
                    className="mr-3"
                    indeterminate
                    dataTestId="header-progress-circular"
                  />
                </motion.div>
              )}
            </AnimatePresence>
            <motion.div layout transition={{ duration: 0.3 }}>
              <div className="flex flex-col">
                <div className="text-emphasis-300 text-text-primary">
                  {toasts.length + " updates in progress"}
                </div>
              </div>
            </motion.div>
          </div>
        </div>
      </div>
      <ResizeablePanel
        className="w-full"
        resizeOn={isExpanded ? toasts.length : false}
      >
        {isExpanded && (
          <>
            <div className="w-full divide-y divide-border-5">
              {toasts.map(({ message, status, id, progress, ttl }) => (
                <GroupedToast
                  key={id}
                  message={message}
                  status={status}
                  progress={progress}
                  ttl={ttl}
                  onClose={() => removeToast(id)}
                />
              ))}
            </div>
            <Button
              className="mt-2 w-full"
              variant={variants.secondary}
              text="Close"
              onClick={() => setIsExpanded(false)}
            />
          </>
        )}
      </ResizeablePanel>
    </div>
  );
};

export default GroupedToaster;
