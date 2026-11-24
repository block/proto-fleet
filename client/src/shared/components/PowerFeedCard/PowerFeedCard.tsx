import type { ReactNode } from "react";
import {
  confidenceLabels,
  mappingReasonText,
  type PowerFeedConfidence,
  type PowerFeedModuleRef,
  type PowerFeedState,
  readingReasonText,
  stateLabels,
} from "./powerFeedText";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import HardwareCardHeader from "@/shared/components/HardwareCardHeader";
import LabeledValue from "@/shared/components/LabeledValue";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

export type PowerFeedReadingView = {
  state: PowerFeedState;
  active?: boolean;
  faulted?: boolean;
  overridden: boolean;
  voltageV?: number;
  currentA?: number;
  powerW?: number;
  /** Line-to-line voltages, ordered A–B, B–C, C–A. */
  lineVoltagesV?: [number | undefined, number | undefined, number | undefined];
  /** Phase currents, ordered A, B, C. */
  phaseCurrentsA?: [number | undefined, number | undefined, number | undefined];
};

export type PowerRecoveryCycleView = {
  phase: string;
  outletIds: string[];
  restoreAfter?: Date;
  lastError: string;
};

export type PowerFeedView = {
  feedId?: string;
  confidence: PowerFeedConfidence;
  reasons: string[];
  sharedWith: PowerFeedModuleRef[];
  reading?: PowerFeedReadingView;
  readingUnavailableReason?: string;
  /** Undefined when the controller's recovery cycle could not be read; null when none is active. */
  recoveryCycle: PowerRecoveryCycleView | null | undefined;
};

const dash = "—";
const fixed = (value: number | undefined, unit: string, digits = 1) =>
  value === undefined ? dash : `${value.toFixed(digits)} ${unit}`;
const triple = (values: (number | undefined)[] | undefined, unit: string) =>
  values?.some((v) => v !== undefined)
    ? `${values.map((v) => (v === undefined ? dash : v.toFixed(1))).join(" / ")} ${unit}`
    : undefined;
const flag = (value: boolean | undefined, yes: string, no: string) => (value === undefined ? dash : value ? yes : no);

/** App-neutral presentation of the feed that supplies one module; the app supplies any header controls. */
export default function PowerFeedCard({
  title = "Power feed",
  feed,
  actions,
}: {
  title?: string;
  feed: PowerFeedView;
  /** Header controls supplied by the app, such as refresh. */
  actions?: ReactNode;
}) {
  const reading = feed.reading;
  const lineVoltages = triple(reading?.lineVoltagesV, "V");
  const phaseCurrents = triple(reading?.phaseCurrentsA, "A");
  const metrics = [
    { label: "Outlet", value: feed.feedId ?? dash },
    { label: "Mapping", value: confidenceLabels[feed.confidence] },
    { label: "State", value: reading ? stateLabels[reading.state] : dash },
    { label: "Output", value: flag(reading?.active, "Active", "Inactive") },
    { label: "Fault", value: flag(reading?.faulted, "Faulted", "None") },
    { label: "Override", value: reading ? (reading.overridden ? "Set" : "None") : dash },
    { label: "Voltage", value: fixed(reading?.voltageV, "V") },
    { label: "Current", value: fixed(reading?.currentA, "A") },
    { label: "Power", value: fixed(reading?.powerW === undefined ? undefined : reading.powerW / 1000, "kW") },
    ...(lineVoltages ? [{ label: "Line voltages", value: lineVoltages }] : []),
    ...(phaseCurrents ? [{ label: "Phase currents", value: phaseCurrents }] : []),
  ];
  const unique = feed.confidence === "wiring_verified" || feed.confidence === "configured";
  const attention = !unique || reading?.faulted === true;
  const notes = [
    ...feed.reasons.map((reason) => mappingReasonText(reason, feed.sharedWith)),
    ...(feed.readingUnavailableReason ? [readingReasonText(feed.readingUnavailableReason)] : []),
    ...(feed.recoveryCycle === undefined ? ["Power recovery status could not be read."] : []),
  ];
  const cycle = feed.recoveryCycle;
  return (
    <article
      aria-label={title}
      data-testid="power-feed-card"
      className="@container flex min-w-0 flex-col gap-6 rounded-xl bg-surface-overlay p-4"
    >
      <HardwareCardHeader
        title={title}
        titleAs="h3"
        actions={actions}
        statusIcon={
          attention ? (
            <span role="img" aria-label={`${title} needs attention`}>
              <Alert width="w-4" className="text-intent-critical-fill" />
            </span>
          ) : null
        }
      />
      {cycle ? (
        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title="Power recovery cycle in progress"
          subtitle={[
            cycle.outletIds.length ? `Outlets ${cycle.outletIds.join(", ")}` : undefined,
            cycle.phase ? `Phase: ${cycle.phase}` : undefined,
            cycle.restoreAfter
              ? `Restores after ${formatTimestamp(cycle.restoreAfter.getTime() / 1000, { includeSeconds: true })}`
              : undefined,
            cycle.lastError ? `Last error: ${cycle.lastError}` : undefined,
          ]
            .filter(Boolean)
            .join(". ")}
        />
      ) : null}
      <div className="grid grid-cols-2 gap-x-4 gap-y-3 @3xl:grid-cols-6">
        {metrics.map((stat) => (
          <LabeledValue key={stat.label} {...stat} />
        ))}
      </div>
      {notes.length ? (
        <ul aria-label={`${title} notes`} className="flex flex-col gap-1 text-200 text-text-primary-70">
          {notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      ) : null}
    </article>
  );
}
