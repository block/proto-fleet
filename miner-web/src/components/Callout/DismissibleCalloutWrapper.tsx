import { ReactNode } from "react";
import clsx from "clsx";

import Callout, { type intents } from "components/Callout";

interface DismissibleCalloutWrapperProps {
  className?: string;
  icon: ReactNode;
  intent: keyof typeof intents;
  onDismiss: () => void;
  show: boolean;
  subtitle: string | ReactNode;
  testId?: string;
}

const DismissibleCalloutWrapper = ({
  className,
  icon,
  intent,
  onDismiss,
  show,
  subtitle,
  testId,
}: DismissibleCalloutWrapperProps) => {
  return (
    <div
      className={clsx(
        "transition-[max-height,margin] ease-in-out duration-200",
        {
          "max-h-0 overflow-hidden": !show,
          "mb-4": show,
          "max-h-96": show,
        },
        className
      )}
      data-testid={testId}
    >
      <Callout
        buttonOnClick={onDismiss}
        buttonText="Dismiss"
        intent={intent}
        prefixIcon={icon}
        subtitle={subtitle}
      />
    </div>
  );
};

export default DismissibleCalloutWrapper;
