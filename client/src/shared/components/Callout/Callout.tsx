import { ReactNode, useMemo } from "react";
import clsx from "clsx";

import { type intents } from "./constants";
import { DismissTiny } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import ButtonGroup, { ButtonProps, groupVariants } from "@/shared/components/ButtonGroup";

interface CalloutProps {
  className?: string;
  buttonOnClick?: () => void;
  buttonText?: string;
  header?: string;
  intent: keyof typeof intents;
  prefixIcon: ReactNode;
  subtitle?: string | ReactNode;
  testId?: string;
  title: string | ReactNode;
  dismissible?: boolean;
  onDismiss?: () => void;
}

const Callout = ({
  className,
  buttonOnClick,
  buttonText,
  header,
  intent,
  prefixIcon,
  subtitle,
  testId = "callout",
  title,
  dismissible = false,
  onDismiss,
}: CalloutProps) => {
  let bgColor;
  switch (intent) {
    case "information":
      bgColor = "bg-intent-info-fill";
      break;
    case "success":
      bgColor = "bg-intent-success-fill";
      break;
    case "warning":
      bgColor = "bg-intent-warning-fill";
      break;
    case "danger":
      bgColor = "bg-intent-critical-fill";
      break;
  }

  const buttons = useMemo(() => {
    const result = [] as ButtonProps[];
    if (buttonText) {
      result.push({
        text: buttonText,
        textColor: "text-current",
        borderColor: "border-current",
        onClick: buttonOnClick,
        variant: dismissible && onDismiss ? variants.primary : variants.secondary,
      });
    }

    if (dismissible && onDismiss) {
      result.push({
        ariaLabel: "Dismiss callout",
        prefixIcon: <DismissTiny />,
        textColor: "text-current",
        borderColor: "border-current",
        onClick: onDismiss,
        variant: variants.secondary,
        className: "!px-1.5 !py-1.5",
      });
    }
    return result;
  }, [buttonOnClick, buttonText, dismissible, onDismiss]);

  let iconColor = "text-intent-info-fill"; // default value
  switch (intent) {
    case "default":
      iconColor = "text-text-primary";
      break;
    case "danger":
      iconColor = "text-intent-critical-fill";
      break;
    case "warning":
      iconColor = "text-intent-warning-fill";
      break;
    case "success":
      iconColor = "text-intent-success-fill";
      break;
    case "information":
      iconColor = "text-intent-info-fill";
      break;
  }

  return (
    <div className={clsx("rounded-xl shadow-100", className)} data-testid={testId}>
      {header && /(information|success|warning|danger)/.test(intent) && (
        <div className={clsx("rounded-t-xl px-4 py-1 text-emphasis-300 text-text-contrast", bgColor)}>{header}</div>
      )}
      <div className="flex rounded-xl bg-surface-elevated-base px-5 py-2.5 text-text-primary">
        <div
          className={clsx("mr-3", iconColor, {
            "mt-1": buttonText || dismissible,
            "mt-0.5": !buttonText && !dismissible,
          })}
        >
          {prefixIcon}
        </div>
        <div className="flex w-full items-center justify-between">
          <div>
            <div className="text-emphasis-300">{title}</div>
            {subtitle && <div className="text-300 text-text-primary-70">{subtitle}</div>}
          </div>
          {buttons.length !== 0 && (
            <div className="ml-4">
              <ButtonGroup
                variant={groupVariants.rightAligned}
                size={sizes.compact}
                buttons={buttons}
                sortButtons={false}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default Callout;
