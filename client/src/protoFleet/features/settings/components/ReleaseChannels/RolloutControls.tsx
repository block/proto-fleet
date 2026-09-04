import type { ReactElement } from "react";
import { create } from "@bufbuild/protobuf";

import { gatesAfterBatch, isPacedMethod, methodOptions, orderOptions, planReadout } from "./behaviorUtils";
import { methodHelpText } from "./rolloutStatus";
import {
  type RolloutAutomationThresholds,
  RolloutAutomationThresholdsSchema,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import Switch from "@/shared/components/Switch";

const parseInt0 = (text: string): number => Math.max(0, Number.parseInt(text, 10) || 0);
const parseOptionalNumber = (text: string): number | undefined => {
  const trimmed = text.trim();
  if (trimmed === "") return undefined;
  const n = Number.parseFloat(trimmed);
  return Number.isFinite(n) && n >= 0 ? n : undefined;
};
const optionalText = (n: number | undefined): string => (n === undefined ? "" : String(n));

interface RolloutControlsProps {
  behavior: RolloutBehavior;
  onChange: (behavior: RolloutBehavior) => void;
  // Miners the channel currently covers, for the plan readout.
  inScopeCount?: number;
  disabled?: boolean;
}

// The "Update behavior" controls, per the release channels design: Method,
// Order, batch sizing, review and auto-continue with its thresholds, and
// the ceiling on miners offline at once. Fields a method cannot use are
// hidden; the server normalizes them away on save as well.
const RolloutControls = ({
  behavior,
  onChange,
  inScopeCount = 0,
  disabled = false,
}: RolloutControlsProps): ReactElement => {
  const update = (patch: Partial<RolloutBehavior>) =>
    onChange(create(RolloutBehaviorSchema, { ...behavior, ...patch }));
  const updateThresholds = (patch: Partial<RolloutAutomationThresholds>) =>
    update({ thresholds: create(RolloutAutomationThresholdsSchema, { ...behavior.thresholds, ...patch }) });

  const thresholds = behavior.thresholds ?? create(RolloutAutomationThresholdsSchema);
  const paced = isPacedMethod(behavior.method);
  const batched = behavior.method === RolloutMethod.BATCHED;
  const pilot = behavior.method === RolloutMethod.PILOT_THEN_CONTINUE;
  const gates = gatesAfterBatch(behavior);
  const readout = planReadout(behavior, inScopeCount);

  return (
    <div className="flex flex-col gap-4" data-testid="rollout-controls">
      <div className="grid grid-cols-2 gap-3 phone:grid-cols-1">
        <Select
          id="rollout-method"
          label="Method"
          options={methodOptions}
          value={String(behavior.method === RolloutMethod.UNSPECIFIED ? RolloutMethod.ALL_AT_ONCE : behavior.method)}
          onChange={(value) => update({ method: Number(value) as RolloutMethod })}
          disabled={disabled}
          testId="rollout-method"
        />
        {paced ? (
          <Select
            id="rollout-order"
            label="Order"
            options={orderOptions}
            value={String(
              behavior.order === RolloutOrder.UNSPECIFIED ? RolloutOrder.LEAST_EFFICIENT_FIRST : behavior.order,
            )}
            onChange={(value) => update({ order: Number(value) as RolloutOrder })}
            disabled={disabled}
            testId="rollout-order"
          />
        ) : null}
      </div>
      <p className="text-200 text-text-primary-70">
        {methodHelpText[behavior.method] || methodHelpText[RolloutMethod.ALL_AT_ONCE]}
      </p>

      {paced ? (
        <div className="grid grid-cols-2 gap-3 phone:grid-cols-1">
          {pilot ? (
            <Input
              id="pilot-size"
              label="Pilot batch size (miners)"
              type="number"
              initValue={behavior.pilotSize}
              onChange={(value) => update({ pilotSize: parseInt0(value) })}
              disabled={disabled}
            />
          ) : (
            <Input
              id="batch-size"
              label="Batch size (miners)"
              type="number"
              initValue={behavior.batchSize}
              onChange={(value) => update({ batchSize: parseInt0(value) })}
              disabled={disabled}
            />
          )}
          {batched && !behavior.reviewAfterEachBatch ? (
            <Input
              id="wait-between-batches"
              label="Wait between batches (minutes)"
              type="number"
              initValue={Math.round(behavior.waitBetweenBatchesSeconds / 60)}
              onChange={(value) => update({ waitBetweenBatchesSeconds: parseInt0(value) * 60 })}
              disabled={disabled}
            />
          ) : null}
        </div>
      ) : null}
      {readout ? <p className="text-200 text-text-primary-50">{readout}</p> : null}

      {batched ? (
        <Switch
          id="review-after-each-batch"
          label="Review after each batch"
          checked={behavior.reviewAfterEachBatch}
          setChecked={(checked) =>
            update({
              reviewAfterEachBatch: typeof checked === "function" ? checked(behavior.reviewAfterEachBatch) : checked,
            })
          }
          disabled={disabled}
        />
      ) : null}

      {gates ? (
        <div className="flex flex-col gap-4 rounded-lg bg-core-primary-5 p-4">
          <Switch
            id="auto-continue"
            label="Auto-continue healthy batches"
            checked={behavior.autoContinueOnHealthyTelemetry}
            setChecked={(checked) =>
              update({
                autoContinueOnHealthyTelemetry:
                  typeof checked === "function" ? checked(behavior.autoContinueOnHealthyTelemetry) : checked,
              })
            }
            disabled={disabled}
          />
          {behavior.autoContinueOnHealthyTelemetry ? (
            <>
              <p className="text-200 text-text-primary-70">
                A reviewed batch continues on its own once every miner is back and hashing, none failed, the limits
                below hold, and telemetry has settled. Leave a limit empty to skip that check.
              </p>
              <div className="grid grid-cols-2 gap-3 phone:grid-cols-1">
                <Input
                  id="max-hashrate-drop"
                  label="Max hashrate drop (%)"
                  type="number"
                  initValue={optionalText(thresholds.maxHashrateDropPercent)}
                  onChange={(value) => updateThresholds({ maxHashrateDropPercent: parseOptionalNumber(value) })}
                  disabled={disabled}
                />
                <Input
                  id="max-efficiency-increase"
                  label="Max efficiency increase (%)"
                  type="number"
                  initValue={optionalText(thresholds.maxEfficiencyIncreasePercent)}
                  onChange={(value) => updateThresholds({ maxEfficiencyIncreasePercent: parseOptionalNumber(value) })}
                  disabled={disabled}
                />
                <Input
                  id="max-temp-increase"
                  label="Max temp increase (°C)"
                  type="number"
                  initValue={optionalText(thresholds.maxTemperatureIncreaseCelsius)}
                  onChange={(value) => updateThresholds({ maxTemperatureIncreaseCelsius: parseOptionalNumber(value) })}
                  disabled={disabled}
                />
                <Input
                  id="max-errors"
                  label="Max errors"
                  type="number"
                  initValue={optionalText(thresholds.maxNewErrors)}
                  onChange={(value) => {
                    const n = parseOptionalNumber(value);
                    updateThresholds({ maxNewErrors: n === undefined ? undefined : Math.round(n) });
                  }}
                  disabled={disabled}
                />
                <Input
                  id="stabilization-minutes"
                  label="Wait for telemetry (minutes)"
                  type="number"
                  initValue={Math.round(behavior.stabilizationSeconds / 60)}
                  onChange={(value) => update({ stabilizationSeconds: parseInt0(value) * 60 })}
                  disabled={disabled}
                />
              </div>
            </>
          ) : null}
        </div>
      ) : null}

      <Input
        id="max-concurrent-offline"
        label="Max miners offline at once (0 for no limit)"
        type="number"
        initValue={behavior.maxConcurrentOffline}
        onChange={(value) => update({ maxConcurrentOffline: parseInt0(value) })}
        disabled={disabled}
      />
    </div>
  );
};

export default RolloutControls;
