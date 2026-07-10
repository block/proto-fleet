import { motion } from "motion/react";
import { ReactNode, useCallback, useRef } from "react";
import clsx from "clsx";

import { variants } from "@/shared/components/Button";
import ButtonGroup from "@/shared/components/ButtonGroup";
import { groupVariants } from "@/shared/components/ButtonGroup/constants";
import { ButtonProps } from "@/shared/components/ButtonGroup/types";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useClickOutsideDismiss } from "@/shared/hooks/useClickOutsideDismiss";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
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
  const footerConfig = getDialogFooterConfig(buttons, buttonGroupVariant);
  const footerButtons = footerConfig.stacked
    ? footerConfig.buttons.map(addDialogMobileStackOrderClass)
    : footerConfig.buttons;

  const dismissDialog = useCallback(() => {
    onDismiss?.();
  }, [onDismiss]);

  useEscapeDismiss(open === false ? undefined : dismissDialog);

  useClickOutsideDismiss({
    ref: dialogRef,
    onDismiss: open === false ? undefined : dismissDialog,
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
            {loading ? (
              <div className="flex w-10 items-center justify-center rounded-lg bg-surface-5 py-2.5">
                <ProgressCircular indeterminate className="text-text-primary" />
              </div>
            ) : null}
            {!loading ? icon : null}
            <Header
              className={headerClassName}
              subtitleClassName={subtitleClassName}
              title={title}
              subtitle={subtitle}
              titleSize={titleSize}
              subtitleSize={subtitleSize}
            />
          </div>
          {children ? <div className="mt-4">{children}</div> : null}
        </div>
        {footerConfig.buttons.length > 0 ? (
          <ButtonGroup
            buttons={footerButtons}
            variant={footerConfig.variant}
            sortButtons={footerConfig.sortButtons}
            fillButtonsOnMobile={!footerConfig.stacked}
            fullWidthButtons={footerConfig.stacked}
            fullWidthOnMobile={footerConfig.stacked}
            className="rounded-b-3xl bg-surface-5 p-6"
          />
        ) : null}
      </motion.div>
    </PageOverlay>
  );
};

const LONG_DIALOG_BUTTON_TEXT_LENGTH = 16;
const closingActionText = new Set(["cancel", "close", "dismiss", "done"]);

function getDialogFooterConfig(buttons: ButtonProps[] | undefined, requestedVariant: keyof typeof groupVariants) {
  if (!buttons || buttons.length === 0) {
    return {
      buttons: [],
      sortButtons: true,
      stacked: false,
      variant: requestedVariant,
    };
  }

  const shouldStack =
    requestedVariant === groupVariants.stack ||
    buttons.length >= 3 ||
    (buttons.length === 2 && buttons.some((button) => (button.text?.length ?? 0) > LONG_DIALOG_BUTTON_TEXT_LENGTH));

  if (!shouldStack) {
    return {
      buttons,
      sortButtons: true,
      stacked: false,
      variant: requestedVariant,
    };
  }

  return {
    buttons: orderStackedDialogButtons(buttons),
    sortButtons: false,
    stacked: true,
    variant: groupVariants.stack,
  };
}

function orderStackedDialogButtons(buttons: ButtonProps[]): ButtonProps[] {
  const primaryActions = buttons.filter(isPrimaryDialogAction);
  const closingActions = buttons.filter((button) => !isPrimaryDialogAction(button) && isClosingDialogAction(button));
  const secondaryActions = buttons.filter((button) => !isPrimaryDialogAction(button) && !isClosingDialogAction(button));
  return [...primaryActions, ...secondaryActions, ...closingActions];
}

function addDialogMobileStackOrderClass(button: ButtonProps): ButtonProps {
  const mobileOrderClassName = isPrimaryDialogAction(button)
    ? "phone:order-1"
    : isClosingDialogAction(button)
      ? "phone:order-3"
      : "phone:order-2";

  return {
    ...button,
    className: clsx(mobileOrderClassName, button.className),
  };
}

function isPrimaryDialogAction(button: ButtonProps): boolean {
  return (
    button.variant === variants.primary || button.variant === variants.danger || button.variant === variants.accent
  );
}

function isClosingDialogAction(button: ButtonProps): boolean {
  return closingActionText.has(button.text?.trim().toLowerCase() ?? "");
}

export default Dialog;
