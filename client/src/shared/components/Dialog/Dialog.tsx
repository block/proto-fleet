import { motion } from "motion/react";
import { ReactNode, useCallback, useRef } from "react";
import clsx from "clsx";

import ButtonGroup from "@/shared/components/ButtonGroup";
import { groupVariants } from "@/shared/components/ButtonGroup/constants";
import { ButtonProps } from "@/shared/components/ButtonGroup/types";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useKeyDown } from "@/shared/hooks/useKeyDown";
import useSlideUpAnimation from "@/shared/hooks/useSlideUpAnimation";

interface DialogProps {
  className?: string;
  children?: ReactNode;
  icon?: ReactNode;
  loading?: boolean;
  preventScroll?: boolean;
  open?: boolean;
  subtitle?: string;
  subtitleClassName?: string;
  subtitleSize?: string;
  testId?: string;
  title: string;
  titleSize?: string;
  headerClassName?: string;
  buttonGroupVariant?: keyof typeof groupVariants;
  buttons?: ButtonProps[];
  onDismiss?: () => void;
}

const Dialog = ({
  className,
  children,
  icon,
  loading,
  preventScroll,
  open,
  subtitle,
  subtitleClassName,
  subtitleSize = "text-heading-100",
  testId,
  title,
  titleSize = "text-heading-300",
  headerClassName,
  buttonGroupVariant = groupVariants.justifyBetween,
  buttons,
  onDismiss,
}: DialogProps) => {
  const dialogRef = useRef<HTMLDivElement>(null);
  const slideUpAnimation = useSlideUpAnimation();

  const dismissDialog = useCallback(() => {
    onDismiss?.();
  }, [onDismiss]);

  const handleEscape = useCallback(() => {
    if (open !== false) {
      dismissDialog();
    }
  }, [open, dismissDialog]);
  useKeyDown({ key: "Escape", onKeyDown: handleEscape });

  const shouldIgnoreClickOutside = useCallback(() => open === false, [open]);
  useClickOutside({
    ref: dialogRef,
    onClickOutside: dismissDialog,
    shouldIgnore: shouldIgnoreClickOutside,
  });

  return (
    <PageOverlay open={open} zIndex="z-60" shouldPreventScroll={preventScroll} position="top">
      <motion.div
        ref={dialogRef}
        {...slideUpAnimation}
        className={clsx("mt-16 h-fit w-108 overflow-hidden rounded-3xl bg-surface-elevated-base shadow-200", className)}
        data-testid={testId}
      >
        <div className="p-6">
          <div className="flex flex-col gap-3">
            {loading && (
              <div className="flex w-10 items-center justify-center rounded-lg bg-surface-5 py-2.5">
                <ProgressCircular indeterminate className="text-text-primary" />
              </div>
            )}
            {!loading && icon}
            <Header
              className={headerClassName}
              subtitleClassName={subtitleClassName}
              title={title}
              subtitle={subtitle}
              titleSize={titleSize}
              subtitleSize={subtitleSize}
            />
          </div>
          {children && <div className="mt-4">{children}</div>}
        </div>
        {buttons && buttons.length > 0 && (
          <ButtonGroup buttons={buttons} variant={buttonGroupVariant} className="rounded-b-3xl bg-surface-5 p-6" />
        )}
      </motion.div>
    </PageOverlay>
  );
};

export default Dialog;
