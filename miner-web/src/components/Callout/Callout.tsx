import { ReactNode } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "components/Button";

import { type intents } from "./constants";

interface CalloutProps {
  className?: string;
  buttonOnClick?: () => void;
  buttonText?: string;
  intent: keyof typeof intents;
  prefixIcon: ReactNode;
  subtitle: string | ReactNode;
}

const Callout = ({
  className,
  buttonOnClick,
  buttonText,
  intent,
  prefixIcon,
  subtitle,
}: CalloutProps) => {
  let bgColor, textColor;
  switch (intent) {
    case "information":
      bgColor = "bg-intent-info-fill/10";
      textColor = "text-intent-info-text";
      break;
    case "success":
      bgColor = "bg-intent-success-fill/10";
      textColor = "text-intent-success-text";
      break;
    case "warning":
      bgColor = "bg-intent-warning-fill/10";
      textColor = "text-intent-warning-text";
      break;
    case "danger":
      bgColor = "bg-intent-critical-fill/10";
      textColor = "text-intent-critical-text";
      break;
    default:
      bgColor = "bg-surface-5";
      textColor = "text-text-primary/70";
  }

  return (
    <div className={clsx("flex p-4 rounded-xl", bgColor, textColor, className)}>
      <div className="mr-3 mt-[2px]">{prefixIcon}</div>
      <div className="flex w-full items-center justify-between">
        <div className="max-w-[600px]">
          <div className="text-300">{subtitle}</div>
        </div>
        {buttonText && (
          <div className="ml-3">
            <Button
              text={buttonText}
              textColor="text-current"
              borderColor="border-current"
              onClick={buttonOnClick}
              size={sizes.textOnly}
              variant={variants.textOnly}
            />
          </div>
        )}
      </div>
    </div>
  );
};

export default Callout;
