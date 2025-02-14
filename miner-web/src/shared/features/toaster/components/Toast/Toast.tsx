import { motion } from "motion/react";
import { useEffect, useState } from "react";
import clsx from "clsx";

import { STATUSES } from "../../constants";
import { type ToastProps } from "../../types";
import { Alert, Dismiss, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

import Spinner from "@/shared/components/Spinner";
import { cubicBezierValues } from "@/shared/utils/cssUtils";
import getTailwindConfig from "@/shared/utils/getTailwindConfig";

const gentle = getTailwindConfig("theme", "transitionTimingFunction", "gentle");
const gentleCb = cubicBezierValues(gentle);

// we need to add a little extra padding on the bottom of the toast
// so that when hovered the gaps between them are still part of the
// parent hover target.  We translate down to compensate
const extraPaddingForHover = 15;
const initialTranslateY = 20;

const Toast = ({
  message,
  onClose,
  status,
  index,
  numToasts,
  ttl = 4000,
}: ToastProps) => {
  const [yOffset, setYOffset] = useState<number>(0);
  const [hoverYOffset, setHoverYOffset] = useState<number>(0);
  const [scale, setScale] = useState<number>(1);

  // If Toast is used outside of toaster and we
  // dont have index or numToast we just assume its on top
  const [onTop, setOnTop] = useState<boolean>(
    index == undefined || numToasts == undefined || index + 1 == numToasts
  );

  useEffect(() => {
    const toID = setTimeout(onClose, ttl);
    return () => {
      clearTimeout(toID);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ttl]);

  useEffect(() => {
    if (numToasts == undefined || index == undefined) {
      return;
    }

    setScale(1 - (numToasts - index - 1) * 0.07);
    setYOffset((numToasts - index - 1) * -14);
    setHoverYOffset((numToasts - index - 1) * -55);
    setOnTop(index + 1 == numToasts);
  }, [index, numToasts]);

  return (
    <motion.div
      className={`absolute bottom-0 right-0 pb-[${extraPaddingForHover}px]`}
      initial={{ opacity: 0, y: initialTranslateY + extraPaddingForHover }}
      animate={{ opacity: 1, scale: scale, y: yOffset + extraPaddingForHover }}
      exit={{
        opacity: 0,
        y: -initialTranslateY + yOffset + extraPaddingForHover,
      }}
      transition={{ duration: 0.3, ease: gentleCb }}
      variants={{ hover: { scale: 1, y: hoverYOffset + extraPaddingForHover } }}
    >
      <div className="w-[400px] p-3 space-x-3 rounded-lg shadow-100 bg-surface-elevated-base">
        <div
          className={clsx(
            "flex transition-opacity duration-200 items-center group-hover:opacity-100",
            onTop ? "opacity-100" : "opacity-0"
          )}
        >
          <div className="flex grow space-x-3 items-center transition-opacity duration-300">
            {status === STATUSES.loading && <Spinner />}
            {status === STATUSES.success && (
              <Success className="text-intent-success-fill" />
            )}
            {status === STATUSES.error && (
              <Alert className="text-intent-warning-fill" />
            )}
            <div className="text-heading-100 text-text-primary">{message}</div>
          </div>
          <button onClick={onClose}>
            <Dismiss className="text-text-primary-30" width={iconSizes.small} />
          </button>
        </div>
      </div>
    </motion.div>
  );
};

export default Toast;
