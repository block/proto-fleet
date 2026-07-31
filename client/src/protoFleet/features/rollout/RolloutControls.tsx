import type { ReactElement } from "react";

import { orderLabels, strategyLabels } from "./rolloutDisplayUtils";
import type { RolloutOrder, RolloutPlanConfig, RolloutStrategy } from "./rolloutTypes";
import { Alert } from "@/shared/assets/icons";
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
 * - pilot then continue — pilot size + a review-gate note, then continuation
 *   batch fields.
 *
 * `maxConcurrentOffline` is a global ceiling, so it is present in every variant.
 * This is a controlled component: it owns no state, mirroring how
 * curtailment's start modal drives its fields.
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

  return (
    <section className="grid gap-3" data-testid="rollout-controls">
      <div>
        <div className="text-emphasis-300 text-text-primary">Rollout</div>
        <div className="text-300 text-text-primary-70">How the update is paced across the selected miners.</div>
      </div>

      <div className="grid gap-4 tablet:grid-cols-2">
        <Select
          id="rollout-strategy"
          label="Rollout strategy"
          options={strategyOptions}
          value={config.strategy}
          onChange={(value) => patch({ strategy: value as RolloutStrategy })}
          disabled={disabled}
          forceBelow
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

      {config.strategy === "allAtOnce" ? (
        <div className="flex items-start gap-3">
          <Alert className="mt-0.5 shrink-0 text-text-primary-50" width="w-5" />
          <span className="text-200 text-text-primary-70">
            All in-scope miners update simultaneously. Fastest, highest uptime impact.
          </span>
        </div>
      ) : null}

      {showPilotFields ? (
        <>
          <Input
            id="rollout-pilot-size"
            label="Pilot group size (miners)"
            type="number"
            inputMode="numeric"
            initValue={config.pilotSize ?? ""}
            onChange={(value) => patch({ pilotSize: parseCount(value) })}
            disabled={disabled}
          />
          <div className="flex items-start gap-3">
            <Alert className="mt-0.5 shrink-0 text-core-accent-fill" width="w-5" />
            <span className="text-200 text-text-primary-70">
              Rollout pauses for your review after the pilot group completes, then continues in batches.
            </span>
          </div>
          <div className="text-emphasis-200 text-text-primary-50">Then continue in batches</div>
        </>
      ) : null}

      {showBatchFields ? (
        <div className="grid gap-4 tablet:grid-cols-2">
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
      ) : null}

      <Input
        id="rollout-max-offline"
        label="Max miners offline at once"
        type="number"
        inputMode="numeric"
        initValue={config.maxConcurrentOffline ?? ""}
        onChange={(value) => patch({ maxConcurrentOffline: parseCount(value) })}
        disabled={disabled}
      />
    </section>
  );
}

export default RolloutControls;
