import { type ReactElement, useState } from "react";
import { create } from "@bufbuild/protobuf";

import {
  gatesAfterBatch,
  hasSampledLimit,
  isPacedMethod,
  methodOptions,
  orderOptions,
  planReadout,
  rolloutBehaviorErrors,
  type RolloutNumericField,
} from "./behaviorUtils";
import { methodHelpText, methodLabels } from "./rolloutStatus";
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

const parseNumberDraft = (text: string, optional: boolean, scale: number): number | undefined => {
  const trimmed = text.trim();
  if (trimmed === "") return optional ? undefined : 0;
  // Parse the whole decimal value; malformed input remains invalid rather
  // than silently removing a limit or accepting only its numeric prefix.
  if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(trimmed)) return Number.NaN;
  const value = Number(trimmed) * scale;
  if (scale !== 1 && Number.isFinite(value)) {
    // Minute/second conversion may land one representable step from an
    // integer (2.05 minutes -> 122.99999999999999 seconds). Correct only
    // floating-point roundoff; actual fractional seconds remain invalid.
    const seconds = Math.round(value);
    if (Math.abs(seconds - value) <= Number.EPSILON * Math.abs(value) * 4) return seconds;
  }
  return value;
};

interface RolloutControlsProps {
  behavior: RolloutBehavior;
  onChange: (behavior: RolloutBehavior) => void;
  // Miners the channel currently covers, for the plan readout.
  inScopeCount?: number;
  disabled?: boolean;
  // Existing delegated channels can switch back before saving another method.
  allowDelegated?: boolean;
}

// The "Update behavior" controls, per the release channels design: Method,
// Order, batch sizing, review and auto-continue with its thresholds, and
// the ceiling on miners offline at once. Fields a method cannot use are
// hidden; the client request builder omits their retained draft values on save.
const RolloutControls = ({
  behavior,
  onChange,
  inScopeCount = 0,
  disabled = false,
  allowDelegated = false,
}: RolloutControlsProps): ReactElement => {
  // Keep raw text above the conditionally mounted fields so hidden invalid
  // edits survive method/gate changes and never become an intentional unset.
  const [numberText, setNumberText] = useState<
    Partial<Record<RolloutNumericField, { text: string; value: number | undefined }>>
  >({});
  const update = (patch: Partial<RolloutBehavior>) =>
    onChange(create(RolloutBehaviorSchema, { ...behavior, ...patch }));
  const updateThresholds = (patch: Partial<RolloutAutomationThresholds>) =>
    update({ thresholds: create(RolloutAutomationThresholdsSchema, { ...behavior.thresholds, ...patch }) });

  const thresholds = behavior.thresholds ?? create(RolloutAutomationThresholdsSchema);
  const paced = isPacedMethod(behavior.method);
  const batched = behavior.method === RolloutMethod.BATCHED;
  const pilot = behavior.method === RolloutMethod.PILOT_THEN_CONTINUE;
  const gates = gatesAfterBatch(behavior);
  const errors = rolloutBehaviorErrors(behavior);
  const readout = errors.batchSize || errors.pilotSize ? null : planReadout(behavior, inScopeCount);
  const numericInput = (
    field: RolloutNumericField,
    value: number | undefined,
    onNumberChange: (value: number | undefined) => void,
    optional = false,
    scale = 1,
  ) => ({
    type: "text",
    inputMode: "decimal" as const,
    initValue:
      numberText[field] && Object.is(numberText[field].value, value)
        ? numberText[field].text
        : value === undefined
          ? ""
          : String(value / scale),
    error: errors[field],
    onChange: (text: string) => {
      const value = parseNumberDraft(text, optional, scale);
      setNumberText((current) => ({ ...current, [field]: { text, value } }));
      onNumberChange(value);
    },
  });
  const availableMethods =
    allowDelegated || behavior.method === RolloutMethod.DELEGATED
      ? [
          ...methodOptions,
          {
            value: String(RolloutMethod.DELEGATED),
            label: methodLabels[RolloutMethod.DELEGATED],
            description: methodHelpText[RolloutMethod.DELEGATED],
          },
        ]
      : methodOptions;

  return (
    <div className="flex flex-col gap-4" data-testid="rollout-controls">
      <div className="grid grid-cols-2 gap-3 phone:grid-cols-1">
        <Select
          id="rollout-method"
          label="Method"
          options={availableMethods}
          value={String(behavior.method === RolloutMethod.UNSPECIFIED ? RolloutMethod.ALL_AT_ONCE : behavior.method)}
          onChange={(value) => update({ method: Number(value) as RolloutMethod })}
          disabled={disabled}
          testId="rollout-method"
        />
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
              {...numericInput("pilotSize", behavior.pilotSize, (value) => update({ pilotSize: value ?? 0 }))}
              disabled={disabled}
            />
          ) : (
            <Input
              id="batch-size"
              label="Batch size (miners)"
              {...numericInput("batchSize", behavior.batchSize, (value) => update({ batchSize: value ?? 0 }))}
              disabled={disabled}
            />
          )}
          {batched && !behavior.reviewAfterEachBatch ? (
            <Input
              id="wait-between-batches"
              label="Wait between batches (minutes)"
              {...numericInput(
                "waitBetweenBatchesSeconds",
                behavior.waitBetweenBatchesSeconds,
                (value) => update({ waitBetweenBatchesSeconds: value ?? 0 }),
                false,
                60,
              )}
              disabled={disabled}
            />
          ) : null}
        </div>
      ) : null}
      {readout ? <p className="text-200 text-text-primary-50">{readout}</p> : null}

      {behavior.method === RolloutMethod.DELEGATED ? (
        <div className="flex flex-col gap-2">
          <dl>
            <dt className="text-200 text-text-primary-70">Controller timeout</dt>
            <dd className="text-200">
              {behavior.controllerTimeoutSeconds === 0
                ? "Never times out"
                : `${behavior.controllerTimeoutSeconds} ${behavior.controllerTimeoutSeconds === 1 ? "second" : "seconds"}`}
            </dd>
          </dl>
          <p className="text-200 text-text-primary-70">
            {behavior.controllerTimeoutSeconds === 0
              ? "Updates can wait indefinitely for controller action."
              : "While waiting for the controller, updates pause after this interval without controller action."}
          </p>
        </div>
      ) : null}

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
                below hold, and telemetry has settled. Leave a maximum empty to skip that check.
              </p>
              <div className="grid grid-cols-2 gap-3 phone:grid-cols-1">
                <Input
                  id="max-hashrate-drop"
                  label="Max hashrate drop (%)"
                  {...numericInput(
                    "maxHashrateDropPercent",
                    thresholds.maxHashrateDropPercent,
                    (value) => updateThresholds({ maxHashrateDropPercent: value }),
                    true,
                  )}
                  disabled={disabled}
                />
                <Input
                  id="max-efficiency-increase"
                  label="Max efficiency increase (%)"
                  {...numericInput(
                    "maxEfficiencyIncreasePercent",
                    thresholds.maxEfficiencyIncreasePercent,
                    (value) => updateThresholds({ maxEfficiencyIncreasePercent: value }),
                    true,
                  )}
                  disabled={disabled}
                />
                <Input
                  id="max-temp-increase"
                  label="Max temp increase (°C)"
                  {...numericInput(
                    "maxTemperatureIncreaseCelsius",
                    thresholds.maxTemperatureIncreaseCelsius,
                    (value) => updateThresholds({ maxTemperatureIncreaseCelsius: value }),
                    true,
                  )}
                  disabled={disabled}
                />
                <Input
                  id="max-errors"
                  label="Max errors"
                  {...numericInput(
                    "maxNewErrors",
                    thresholds.maxNewErrors,
                    (value) => updateThresholds({ maxNewErrors: value }),
                    true,
                  )}
                  disabled={disabled}
                />
                {hasSampledLimit(thresholds) ? (
                  <div className="flex flex-col gap-2">
                    <Input
                      id="min-sample-coverage"
                      label="Min sample coverage (%)"
                      {...numericInput(
                        "minSampleCoveragePercent",
                        thresholds.minSampleCoveragePercent,
                        (value) => updateThresholds({ minSampleCoveragePercent: value }),
                        true,
                      )}
                      disabled={disabled}
                    />
                    <p className="text-200 text-text-primary-70">
                      Minimum share of verified miners each enabled hashrate, efficiency, or temperature check must
                      sample. Empty means 100%; error counts do not use this setting.
                    </p>
                  </div>
                ) : null}
                <Input
                  id="stabilization-minutes"
                  label="Wait for telemetry (minutes)"
                  {...numericInput(
                    "stabilizationSeconds",
                    behavior.stabilizationSeconds,
                    (value) => update({ stabilizationSeconds: value ?? 0 }),
                    false,
                    60,
                  )}
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
        {...numericInput("maxConcurrentOffline", behavior.maxConcurrentOffline, (value) =>
          update({ maxConcurrentOffline: value ?? 0 }),
        )}
        disabled={disabled}
      />
    </div>
  );
};

export default RolloutControls;
