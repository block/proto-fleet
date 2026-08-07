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
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

interface RolloutControlsProps {
  config: RolloutPlanConfig;
  onChange: (next: RolloutPlanConfig) => void;
  /** Read-only presentation (e.g. inside a summary) — disables every field. */
  disabled?: boolean;
  /** In-scope target count (total minus excluded). When supplied, the control
   * shows a live plan readout ("≈ 12 batches over ~11m") so an operator doesn't
   * have to work out wave count / duration from the batch size + interval by
   * hand. Omit when the scope isn't known yet at this surface. */
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

/**
 * The contextual pacing panel injected into a process's config modal (firmware,
 * reboot, …), between the "Apply to" scope and the "Date and time" schedule.
 * Its field-set changes with the chosen strategy:
 *
 * - all at once — no pacing fields; just strategy + order + the offline ceiling.
 * - batched — adds batch size + interval.
 * - pilot then continue — pilot size, then continuation batch fields.
 *
 * The per-strategy explanation lives in the strategy field's info popover (the
 * curtailment field-help pattern) rather than as inline copy, so the control
 * stays compact. Fields pair two-up wherever there's a natural partner —
 * strategy + order always, batch size + interval, and pilot size + the offline
 * ceiling — falling back to a lone full-width field when there's nothing to
 * pair with (the same lone-vs-pair reflow curtailment uses). `maxConcurrentOffline`
 * is a global ceiling, so it appears in every variant. Controlled component:
 * owns no state, mirroring curtailment's start modal.
 */
function RolloutControls({ config, onChange, disabled = false, inScopeCount }: RolloutControlsProps): ReactElement {
  function patch(partial: Partial<RolloutPlanConfig>): void {
    onChange({ ...config, ...partial });
  }

  function parseCount(value: string): number {
    const parsed = Number.parseInt(value, 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
  }

  const showBatchFields = config.strategy === "batched" || config.strategy === "pilotThenContinue";
  const showPilotFields = config.strategy === "pilotThenContinue";
  // Order only bites when the run is paced — under "all at once" there is no
  // first/last, so the control is meaningless and we hide it (the design-review
  // call). When hidden, Method spans full width rather than leaving an orphaned
  // half-row.
  const showOrder = config.strategy !== "allAtOnce";

  // Live plan readout — only when the host knows the in-scope count. Reduces the
  // manual "how many waves / how long?" math the design review flagged.
  const planReadout = inScopeCount !== undefined ? rolloutPlanReadout({ inScopeCount, config }) : null;

  // Present-tense action verb ("update" / "reboot" / "curtail") for the copy,
  // so the section subtext and strategy help read for whichever process this
  // control is driving rather than hardcoding firmware's "update".
  const verb = rolloutProcessVerb(config.processType);
  // Action-prefixed section title ("Update behavior" / "Reboot behavior"),
  // adopting curtailment's "behavior" vocabulary across bulk workflows.
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
          onChange={(value) => patch({ strategy: value as RolloutStrategy })}
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
