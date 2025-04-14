import { ReactNode, useMemo } from "react";
import clsx from "clsx";

import { type intents } from "./constants";
import { DismissTiny } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import ButtonGroup, {
  ButtonProps,
  groupVariants,
} from "@/shared/components/ButtonGroup";

interface CalloutProps {
  className?: string;
  buttonOnClick?: () => void;
  buttonText?: string;
  header?: string;
  intent: keyof typeof intents;
  prefixIcon: ReactNode;
  subtitle?: string | ReactNode;
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
        variant:
          dismissible && onDismiss ? variants.primary : variants.secondary,
      });
    }

    if (dismissible && onDismiss) {
      result.push({
        prefixIcon: <DismissTiny />,
        textColor: "text-current",
        borderColor: "border-current",
        onClick: onDismiss,
        variant: variants.secondary,
      });
    }
    return result;
  }, [buttonOnClick, buttonText, dismissible, onDismiss]);

  return (
    <div className={clsx("rounded-xl shadow-100", className)}>
      {header && /(information|success|warning|danger)/.test(intent) && (
        <div
          className={clsx(
            "rounded-t-xl px-4 py-1 text-emphasis-300 text-text-contrast",
            bgColor,
          )}
        >
          {header}
        </div>
      )}
      <div className="flex rounded-xl bg-surface-elevated-base p-4 text-text-primary-70">
        <div
          className={clsx("mr-3", {
            "mt-1": buttonText,
            "mt-[2px]": !buttonText,
          })}
        >
          {prefixIcon}
        </div>
        <div className="flex w-full items-center justify-between">
          <div>
            <div className="text-emphasis-300">{title}</div>
            {subtitle && <div className="text-300">{subtitle}</div>}
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
