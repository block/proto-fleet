import type { ReactElement } from "react";

import { rolloutErrorCount, rolloutErrorImpactCount } from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import { Alert } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

function errorSummaryLabel(errorCount: number, impactedMinerCount: number): string {
  const errorWord = errorCount === 1 ? "error" : "errors";
  const minerWord = impactedMinerCount === 1 ? "miner" : "miners";
  return `${errorCount.toLocaleString()} ${errorWord} affecting ${impactedMinerCount.toLocaleString()} ${minerWord}`;
}

/** Standard rollout error treatment with a direct path to impacted miners. */
export default function RolloutErrorCallout({
  event,
  onReviewErrors,
  className,
}: {
  event: RolloutEvent;
  onReviewErrors?: () => void;
  className?: string;
}): ReactElement | null {
  const errorCount = rolloutErrorCount(event.errors);
  if (errorCount === 0) {
    return null;
  }

  const impactedMinerCount = rolloutErrorImpactCount(event.errors);
  return (
    <Callout
      className={className ?? "mt-6"}
      intent={intents.danger}
      prefixIcon={<Alert />}
      testId="active-rollout-error-banner"
      title={errorSummaryLabel(errorCount, impactedMinerCount)}
      subtitle="Review impacted miners before continuing."
      buttonText={onReviewErrors ? "Review errors" : undefined}
      buttonOnClick={onReviewErrors}
    />
  );
}
