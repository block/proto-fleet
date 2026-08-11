import type { ReactElement } from "react";

import {
  orderLabels,
  rolloutBehaviorLabel,
  rolloutPlanReadout,
  rolloutProcessVerb,
  strategyHelpText,
  strategyLabels,
} from "./rolloutDisplayUtils";
import RolloutFieldInfo from "./RolloutFieldInfo";
import type { RolloutOrder, RolloutPlanConfig, RolloutStrategy } from "./rolloutTypes";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

interface RolloutControlsProps {
  config: RolloutPlanConfig;
  onChange: (next: RolloutPlanConfig) => void;
  /** Read-only presentation (e.g. inside a summary), disables every field. */
  disabled?: boolean;
  /** In-scope target count for the live plan readout. */
  inScopeCount?: number;
}

const strategyOptions = (Object.keys(strategyLabels) as RolloutStrategy[]).map((value) => ({
  value,
  label: strategyLabels[value],
}));

const orderOptions = (Object.keys(orderLabels) as RolloutOrder[]).map((value) => ({
  value,
  label: orderLabels[value],
}));

/** Pacing controls shared by rollout config surfaces. */
function RolloutControls({ config, onChange, disabled = false, inScopeCount }: RolloutControlsProps): ReactElement {
  function patch(partial: Partial<RolloutPlanConfig>): void {
    onChange({ ...config, ...partial });
  }

  function parseCount(value: string): number {
    const parsed = Number.parseInt(value, 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
  }

  function patchStrategy(value: string): void {
    const strategy = value as RolloutStrategy;
    patch({
      strategy,
      reviewAfterEachBatch: strategy === "allAtOnce" ? false : config.reviewAfterEachBatch,
    });
  }

  const showBatchFields = config.strategy === "batched" || config.strategy === "pilotThenContinue";
  const showPilotFields = config.strategy === "pilotThenContinue";
  // Order only applies to paced runs. When hidden, Method spans the full row.
  const showOrder = config.strategy !== "allAtOnce";

  // Live plan readout, only when the host knows the in-scope count.
  const planReadout = inScopeCount !== undefined ? rolloutPlanReadout({ inScopeCount, config }) : null;

  // Present-tense action verb ("update" / "reboot" / "curtail") for copy.
  const verb = rolloutProcessVerb(config.processType);
  // Action-prefixed section title ("Update behavior" / "Reboot behavior").
  const behaviorLabel = rolloutBehaviorLabel(config.processType);
  const strategyHelp = strategyHelpText(config.processType);

  const pilotField = (
    <Input
      id="rollout-pilot-size"
      label="Pilot group size (miners)"
      type="number"
      inputMode="numeric"
      initValue={config.pilotSize ?? ""}
      onChange={(value) => patch({ pilotSize: parseCount(value) })}
      disabled={disabled}
    />
  );

  const batchFields = (
    <div className="grid gap-3 tablet:grid-cols-2">
      <Input
        id="rollout-batch-size"
        label="Batch size (miners)"
        type="number"
        inputMode="numeric"
        initValue={config.batchSize ?? ""}
        onChange={(value) => patch({ batchSize: parseCount(value) })}
        disabled={disabled}
      />
      <Input
        id="rollout-batch-interval"
        label="Wait between batches"
        type="number"
        inputMode="numeric"
        units="sec"
        initValue={config.batchIntervalSec ?? ""}
        onChange={(value) => patch({ batchIntervalSec: parseCount(value) })}
        disabled={disabled}
      />
    </div>
  );

  const reviewAfterEachBatchControl = showBatchFields ? (
    <div className="flex items-center gap-2" data-testid="rollout-review-after-each-batch-control">
      <label className={`flex items-center gap-3 text-left ${disabled ? "cursor-not-allowed" : "cursor-pointer"}`}>
        <Checkbox
          checked={config.reviewAfterEachBatch ?? false}
          disabled={disabled}
          onChange={(event) => patch({ reviewAfterEachBatch: event.currentTarget.checked })}
        />
        <span className="text-300 text-text-primary">Review after each batch</span>
      </label>
      <RolloutFieldInfo
        ariaLabel="About batch review"
        body="Pauses when each batch completes so you can review telemetry before continuing."
        testId="rollout-review-after-each-batch-info-button"
        popoverTestId="rollout-review-after-each-batch-info-popover"
      />
    </div>
  ) : null;

  const maxOfflineField = (
    <Input
      id="rollout-max-offline"
      label="Max miners offline at once"
      type="number"
      inputMode="numeric"
      initValue={config.maxConcurrentOffline ?? ""}
      onChange={(value) => patch({ maxConcurrentOffline: parseCount(value) })}
      disabled={disabled}
    />
  );

  return (
    <section className="grid gap-3" data-testid="rollout-controls">
      <div>
        <div className="text-emphasis-300 text-text-primary">{behaviorLabel}</div>
        <div className="text-300 text-text-primary-70">How the {verb} is paced across the selected miners.</div>
      </div>

      <div className={showOrder ? "grid gap-3 tablet:grid-cols-2" : "grid gap-3"}>
        <Select
          id="rollout-strategy"
          label="Method"
          options={strategyOptions}
          value={config.strategy}
          onChange={patchStrategy}
          disabled={disabled}
          forceBelow
          suffixAction={
            <RolloutFieldInfo
              ariaLabel="About pacing methods"
              body={strategyHelp[config.strategy]}
              testId="rollout-strategy-info-button"
              popoverTestId="rollout-strategy-info-popover"
            />
          }
        />

        {showOrder ? (
          <Select
            id="rollout-order"
            label="Order"
            options={orderOptions}
            value={config.order}
            onChange={(value) => patch({ order: value as RolloutOrder })}
            disabled={disabled}
            forceBelow
          />
        ) : null}
      </div>

      {showPilotFields ? (
        <>
          {/* Pilot phase size pairs with the global offline ceiling. */}
          <div className="grid gap-3 tablet:grid-cols-2">
            {pilotField}
            {maxOfflineField}
          </div>
          {batchFields}
        </>
      ) : showBatchFields ? (
        <>
          {batchFields}
          {maxOfflineField}
        </>
      ) : (
        maxOfflineField
      )}

      {reviewAfterEachBatchControl}

      {/* Live plan readout: turns the entered batch size + interval into the
          wave count and duration so the operator doesn't do the math. Only
          shown when the host supplies the in-scope count and the plan is
          complete enough to summarize. */}
      {planReadout ? (
        <div className="text-200 text-text-primary-70" data-testid="rollout-plan-readout">
          {planReadout}
        </div>
      ) : null}
    </section>
  );
}

export default RolloutControls;
