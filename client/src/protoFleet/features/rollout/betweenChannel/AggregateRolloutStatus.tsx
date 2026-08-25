import BetweenChannelRolloutStatus from "@/protoFleet/features/rollout/betweenChannel/BetweenChannelRolloutStatus";
import type { RolloutGroup, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
import { Alert } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

export interface RolloutChildMutationState {
  loading: boolean;
  error?: string;
}

interface AggregateRolloutStatusProps {
  parent: RolloutGroup;
  children: RolloutRecord[];
  focusedChildId: string | null;
  laneLabel: string;
  canControl: boolean;
  childMutationState?: Record<string, RolloutChildMutationState>;
  onFocusChange: (childId: string | null) => void;
  onAdmit: (child: RolloutRecord) => void;
  onPause: (child: RolloutRecord) => void;
  onResume: (child: RolloutRecord) => void;
  onContinue: (child: RolloutRecord, reason?: string) => void;
  onAbort: (child: RolloutRecord) => void;
  onRevert: (child: RolloutRecord) => void;
  onCompleteWithFailures: (child: RolloutRecord) => void;
}

export default function AggregateRolloutStatus({
  parent,
  children,
  focusedChildId,
  laneLabel,
  canControl,
  childMutationState = {},
  onFocusChange,
  onAdmit,
  onPause,
  onResume,
  onContinue,
  onAbort,
  onRevert,
  onCompleteWithFailures,
}: AggregateRolloutStatusProps) {
  return (
    <section className="grid gap-4" aria-label={`Aggregate rollout ${parent.name}`}>
      <div
        className="grid gap-2 rounded-2xl border border-border-5 bg-surface-overlay p-5"
        data-testid="rollout-parent-summary"
      >
        <div className="text-200 text-text-primary-50">Overall rollout</div>
        <div className="text-heading-200 text-text-primary">{parent.name}</div>
        <div className="text-300 text-text-primary-70">
          {children.length.toLocaleString()} selected model
          {children.length === 1 ? "" : "s"} · {(parent.activity ?? "created").replace(/([A-Z])/g, " $1").toLowerCase()}
        </div>
        <div className="text-200 text-text-primary-70">Controls are available on each model rollout below.</div>
        {parent.lifecycle === "terminal" ? (
          <div className="grid gap-2 border-t border-border-5 pt-3">
            <div className="text-emphasis-300 text-text-primary">
              Result: {parent.terminalOutcome.replace(/([A-Z])/g, " $1").toLowerCase()}
            </div>
            {parent.models.map((model) => (
              <div key={model.laneModelId} className="text-200 text-text-primary-70">
                {model.manufacturer} {model.model}: channel {model.sourceChannelId.toString()} to{" "}
                {model.targetChannelId.toString()} · {model.memberCount.toLocaleString()} miner
                {model.memberCount === 1 ? "" : "s"}
              </div>
            ))}
          </div>
        ) : null}
      </div>
      <div
        className="sr-only"
        role="status"
        aria-live="polite"
        aria-atomic="true"
        data-testid="aggregate-rollout-live-region"
      >
        {children.map((child) => `${child.manufacturer} ${child.model}: ${child.state}`).join(". ")}
      </div>
      {children.map((child) => {
        const expanded = focusedChildId === child.id;
        const modelLabel = [child.manufacturer, child.model].filter(Boolean).join(" ") || child.modelIdentityKey;
        const panelId = `rollout-child-${child.id}`;
        const localMutation = childMutationState[child.id];
        return (
          <section
            key={child.id}
            className="grid gap-3 rounded-2xl border border-border-5 bg-surface-base p-4 phone:p-3"
            aria-label={`${modelLabel} rollout`}
          >
            <button
              type="button"
              className="flex min-h-11 items-center justify-between gap-3 text-left"
              aria-expanded={expanded}
              aria-controls={panelId}
              onClick={() => onFocusChange(expanded ? null : child.id)}
            >
              <span>
                <span className="block text-emphasis-300 text-text-primary">{modelLabel}</span>
                <span className="block text-200 text-text-primary-70">
                  {child.state.replace(/([A-Z])/g, " $1").toLowerCase()} ·{" "}
                  {(child.memberCount ?? child.members.length).toLocaleString()} miner
                  {(child.memberCount ?? child.members.length) === 1 ? "" : "s"}
                </span>
              </span>
              <span aria-hidden>{expanded ? "−" : "+"}</span>
            </button>
            {localMutation?.error ? (
              <Callout
                intent={intents.danger}
                prefixIcon={<Alert />}
                title={`${modelLabel} needs attention`}
                subtitle={localMutation.error}
              />
            ) : null}
            {expanded ? (
              <div id={panelId}>
                <BetweenChannelRolloutStatus
                  rollout={child}
                  laneLabel={laneLabel}
                  canControl={canControl}
                  isMutating={Boolean(localMutation?.loading)}
                  announceEvidenceStatus={false}
                  onAdmit={() => onAdmit(child)}
                  onPause={() => onPause(child)}
                  onResume={() => onResume(child)}
                  onContinue={(reason) => onContinue(child, reason)}
                  onAbort={() => onAbort(child)}
                  onRevert={() => onRevert(child)}
                  onCompleteWithFailures={() => onCompleteWithFailures(child)}
                />
              </div>
            ) : null}
          </section>
        );
      })}
    </section>
  );
}
