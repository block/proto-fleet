import type { ReactElement } from "react";

import { orderLabels, strategyHelpText, strategyLabels } from "./rolloutDisplayUtils";
import RolloutFieldInfo from "./RolloutFieldInfo";
import type { RolloutOrder, RolloutPlanConfig, RolloutStrategy } from "./rolloutTypes";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

interface RolloutControlsProps {
  config: RolloutPlanConfig;
  onChange: (next: RolloutPlanConfig) => void;
  /** Read-only presentation (e.g. inside a summary) — disables every field. */
  disabled?: boolean;
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
function RolloutControls({ config, onChange, disabled = false }: RolloutControlsProps): ReactElement {
  function patch(partial: Partial<RolloutPlanConfig>): void {
    onChange({ ...config, ...partial });
  }

  function parseCount(value: string): number {
    const parsed = Number.parseInt(value, 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
  }

  const showBatchFields = config.strategy === "batched" || config.strategy === "pilotThenContinue";
  const showPilotFields = config.strategy === "pilotThenContinue";

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
        label="Batch interval (sec)"
        type="number"
        inputMode="numeric"
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
        <div className="text-emphasis-300 text-text-primary">Rollout</div>
        <div className="text-300 text-text-primary-70">How the update is paced across the selected miners.</div>
      </div>

      <div className="grid gap-3 tablet:grid-cols-2">
        <Select
          id="rollout-strategy"
          label="Rollout strategy"
          options={strategyOptions}
          value={config.strategy}
          onChange={(value) => patch({ strategy: value as RolloutStrategy })}
          disabled={disabled}
          forceBelow
          suffixAction={
            <RolloutFieldInfo
              ariaLabel="About rollout strategies"
              body={strategyHelpText[config.strategy]}
              testId="rollout-strategy-info-button"
              popoverTestId="rollout-strategy-info-popover"
            />
          }
        />

        <Select
          id="rollout-order"
          label="Rollout order"
          options={orderOptions}
          value={config.order}
          onChange={(value) => patch({ order: value as RolloutOrder })}
          disabled={disabled}
          forceBelow
        />
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
    </section>
  );
}

export default RolloutControls;
