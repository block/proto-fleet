import { motion } from "motion/react";
import { useEffect } from "react";
import clsx from "clsx";

import { defaultTtl, STATUSES } from "../../constants";
import { type ToastProps } from "../../types";
import { Alert, Dismiss, Info, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

import ProgressCircular from "@/shared/components/ProgressCircular";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

// we need to add a little extra padding on the bottom of the toast
// so that when hovered the gaps between them are still part of the
// parent hover target.  We translate down to compensate
const extraPaddingForHover = 15;
const initialTranslateY = 20;

const Toast = ({ message, onClick, onClose, status, index, numToasts, ttl = defaultTtl }: ToastProps) => {
  // If Toast is used outside of toaster and we don't have index or numToasts
  // we just assume it's on top with no stacking transform applied.
  const stackOffset = index !== undefined && numToasts !== undefined ? numToasts - index - 1 : 0;
  const scale = 1 - stackOffset * 0.07;
  const yOffset = stackOffset * -14;
  const hoverYOffset = stackOffset * -55;
  const onTop = index == undefined || numToasts == undefined || index + 1 == numToasts;

  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);
  const icon = (
    <>
      {status === STATUSES.loading ? <ProgressCircular indeterminate /> : null}
      {status === STATUSES.success ? <Success className="text-intent-success-fill" /> : null}
      {status === STATUSES.info ? <Info className="text-intent-info-fill" /> : null}
      {status === STATUSES.error ? <Alert className="text-intent-critical-fill" /> : null}
    </>
  );
  const messageContent = (
    <>
      {icon}
      <div className="text-left text-heading-100 text-text-primary">{message}</div>
    </>
  );

  useEffect(() => {
    if (ttl !== false) {
      const toID = setTimeout(onClose, ttl);
      return () => {
        clearTimeout(toID);
      };
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ttl]);

  return (
    <motion.div
      className={`absolute right-0 bottom-0 pb-[${extraPaddingForHover}px]`}
      initial={{ opacity: 0, y: initialTranslateY + extraPaddingForHover }}
      animate={{ opacity: 1, scale: scale, y: yOffset + extraPaddingForHover }}
      exit={{
        opacity: 0,
        y: -initialTranslateY + yOffset + extraPaddingForHover,
      }}
      transition={{ duration: 0.3, ease: easeGentle }}
      variants={{ hover: { scale: 1, y: hoverYOffset + extraPaddingForHover } }}
    >
      <div
        className="w-100 max-w-[calc(100vw-1rem)] space-x-3 rounded-lg bg-surface-elevated-base p-3 shadow-100"
        data-testid="toast"
      >
        <div
          className={clsx(
            "flex items-center transition-opacity duration-200 group-hover:opacity-100",
            onTop ? "opacity-100" : "opacity-0",
          )}
        >
          {onClick ? (
            <button
              type="button"
              onClick={onClick}
              className="flex grow cursor-pointer items-center space-x-3 rounded-sm text-left transition-opacity duration-300 outline-none hover:opacity-80 focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-elevated-base"
            >
              {messageContent}
            </button>
          ) : (
            <div className="flex grow items-center space-x-3 transition-opacity duration-300">{messageContent}</div>
          )}
          <button type="button" onClick={onClose}>
            <Dismiss className="text-text-primary-30" width={iconSizes.small} />
          </button>
        </div>
      </div>
    </motion.div>
  );
};

export default Toast;
