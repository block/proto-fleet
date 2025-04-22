import { ReactNode } from "react";
import clsx from "clsx";

import Callout, { type intents } from "@/shared/components/Callout";

interface DismissibleCalloutWrapperProps {
  className?: string;
  icon: ReactNode;
  intent: keyof typeof intents;
  onDismiss: () => void;
  show: boolean;
  subtitle?: string | ReactNode;
  testId?: string;
  title: string | ReactNode;
}

const DismissibleCalloutWrapper = ({
  className,
  icon,
  intent,
  onDismiss,
  show,
  subtitle,
  testId,
  title,
}: DismissibleCalloutWrapperProps) => {
  return (
    <div
      className={clsx(
        "transition-[max-height,margin] duration-200 ease-in-out",
        {
          "max-h-0 overflow-hidden": !show,
          "mb-4": show,
          "max-h-96": show,
        },
        className,
      )}
      data-testid={testId}
    >
      <Callout
        buttonOnClick={onDismiss}
        buttonText="Dismiss"
        intent={intent}
        prefixIcon={icon}
        subtitle={subtitle}
        title={title}
      />
    </div>
  );
};

export default DismissibleCalloutWrapper;
