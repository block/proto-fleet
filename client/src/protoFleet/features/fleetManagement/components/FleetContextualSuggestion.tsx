import { type ReactNode } from "react";
import clsx from "clsx";

import { DismissTiny, Info } from "@/shared/assets/icons";
import Button, { type ButtonVariant, sizes, variants } from "@/shared/components/Button";

export type FleetContextualSuggestionAction = {
  label: string;
  onClick: () => void;
  variant?: ButtonVariant;
  disabled?: boolean;
  testId?: string;
};

type FleetContextualSuggestionProps = {
  className?: string;
  title: string;
  detail?: string;
  icon?: ReactNode;
  action: FleetContextualSuggestionAction;
  onDismiss?: () => void;
  testId?: string;
};

const renderAction = (action: FleetContextualSuggestionAction) => (
  <Button
    key={action.label}
    variant={action.variant ?? variants.primary}
    size={sizes.compact}
    className="min-h-8"
    onClick={action.onClick}
    disabled={action.disabled}
    testId={action.testId}
  >
    {action.label}
  </Button>
);

const FleetContextualSuggestion = ({
  className,
  title,
  detail,
  icon = <Info width="w-4" />,
  action,
  onDismiss,
  testId = "fleet-contextual-suggestion",
}: FleetContextualSuggestionProps) => (
  <div
    className={clsx(
      "flex flex-col gap-4 rounded-lg border border-border-5 bg-surface-elevated-base p-4 shadow-50 tablet:flex-row tablet:items-center tablet:justify-between",
      className,
    )}
    data-testid={testId}
  >
    <div className="flex min-w-0 gap-3">
      <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-core-primary-5 text-text-primary-70">
        {icon}
      </div>
      <div className="min-w-0">
        <div className="text-emphasis-300 text-text-primary">{title}</div>
        {detail ? <div className="mt-1 text-300 text-text-primary-70">{detail}</div> : null}
      </div>
    </div>
    <div className="flex shrink-0 flex-wrap items-center gap-2 pl-11 tablet:justify-end tablet:pl-0">
      {renderAction(action)}
      {onDismiss ? (
        <Button
          variant={variants.ghost}
          size={sizes.compact}
          ariaLabel="Dismiss suggestion"
          className="min-h-8"
          prefixIcon={<DismissTiny />}
          onClick={onDismiss}
          testId={`${testId}-dismiss`}
        />
      ) : null}
    </div>
  </div>
);

export default FleetContextualSuggestion;
