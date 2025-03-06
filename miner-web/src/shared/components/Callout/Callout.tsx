import { ReactNode } from "react";
import clsx from "clsx";

import { type intents } from "./constants";
import Button, { sizes, variants } from "@/shared/components/Button";

interface CalloutProps {
  className?: string;
  buttonOnClick?: () => void;
  buttonText?: string;
  header?: string;
  intent: keyof typeof intents;
  prefixIcon: ReactNode;
  subtitle?: string | ReactNode;
  title: string | ReactNode;
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

  return (
    <div className={clsx("shadow-100 rounded-xl", className)}>
      {header && /(information|success|warning|danger)/.test(intent) && (
        <div
          className={clsx(
            "rounded-t-xl px-4 py-1 text-text-contrast text-emphasis-300",
            bgColor,
          )}
        >
          {header}
        </div>
      )}
      <div className="flex p-4 text-text-primary-70 bg-surface-elevated-base rounded-xl">
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
          {buttonText && (
            <div className="ml-4">
              <Button
                text={buttonText}
                textColor="text-current"
                borderColor="border-current"
                onClick={buttonOnClick}
                size={sizes.compact}
                variant={variants.secondary}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default Callout;
