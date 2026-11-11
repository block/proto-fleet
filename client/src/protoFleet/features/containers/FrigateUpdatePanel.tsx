import { useCallback, useRef, useState } from "react";
import {
  describeFrigateError,
  FRIGATE_UPDATE_RUNBOOK_URL,
  frigateAgentPhaseLabel,
  frigateRolloutPhaseLabel,
  frigateStageLabel,
  frigateStateLabel,
  timestampSeconds,
} from "./frigateUpdate";
import {
  FrigateAgentPhase,
  FrigateUpdateState,
  type InstanceProgress,
  InstanceStageState,
  type UpdateJob,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isFrigateUpdateActive, useFrigateUpdateActions } from "@/protoFleet/api/useFrigateUpdates";
import { useTicketOperationKey } from "@/protoFleet/features/maintenance/idempotency";
import { Alert, Info } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type Confirming = "retry" | "cancel" | undefined;

const formatContact = (seconds?: number) => (seconds ? formatTimestamp(seconds, { includeSeconds: true }) : "never");

function InstanceRow({ instance, driver }: { instance: InstanceProgress; driver: boolean }) {
  const staging = instance.stageState === InstanceStageState.STAGING && instance.sizeBytes > 0n;
  const failure =
    instance.errorCode || instance.errorMessage
      ? describeFrigateError(instance.errorCode, instance.errorMessage)
      : undefined;
  const contact = timestampSeconds(instance.lastContact);
  const phase =
    instance.agentPhase === FrigateAgentPhase.UNSPECIFIED
      ? frigateStageLabel(instance.stageState) || "Not reported"
      : frigateAgentPhaseLabel(instance.agentPhase);
  return (
    <li
      className="flex flex-col gap-1 rounded-lg border border-border-5 p-3"
      data-testid={`frigate-instance-${instance.instanceId}`}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-emphasis-300 text-text-primary">{instance.instanceId}</span>
        {driver ? <span className="text-200 text-text-primary-70">Driving the update</span> : null}
      </div>
      <div className="text-300 text-text-primary">
        {phase}
        {staging
          ? `, ${formatFileSize(Number(instance.stagedBytes))} of ${formatFileSize(Number(instance.sizeBytes))}`
          : ""}
      </div>
      <div className="flex flex-wrap items-center gap-2 text-200 text-text-primary-70">
        <span>Last contact {formatContact(contact)}</span>
        {instance.contactStale ? (
          <span className="text-intent-warning-text" data-testid={`frigate-instance-${instance.instanceId}-stale`}>
            No report for over 30 seconds
          </span>
        ) : null}
        {instance.runningVersion ? <span>Frigate {instance.runningVersion}</span> : null}
      </div>
      {failure ? (
        <div role="alert" className="text-200 text-intent-critical-text">
          {failure.title}
          {failure.detail ? ` ${failure.detail}` : ""} Code: {failure.code || "unknown"}
        </div>
      ) : null}
    </li>
  );
}

function ConfirmDialog({
  kind,
  busy,
  error,
  onConfirm,
  onDismiss,
}: {
  kind: Exclude<Confirming, undefined>;
  busy: boolean;
  error?: string;
  onConfirm: () => void;
  onDismiss: () => void;
}) {
  const retry = kind === "retry";
  return (
    <Dialog
      open
      title={retry ? "Retry the Frigate update?" : "Cancel the Frigate update?"}
      testId={`frigate-update-${kind}-dialog`}
      onDismiss={busy ? undefined : onDismiss}
      buttons={[
        { text: retry ? "Not now" : "Keep update", variant: variants.secondary, onClick: onDismiss, disabled: busy },
        {
          text: retry ? "Retry update" : "Cancel update",
          variant: retry ? variants.primary : variants.danger,
          onClick: onConfirm,
          loading: busy,
        },
      ]}
    >
      <p className="text-300 text-text-primary-70">
        {retry
          ? "The update resumes from the step that failed. Both controllers stop mining control together, briefly."
          : "No controller has been stopped, so cancelling leaves Frigate unchanged. You can start a new update afterwards."}
      </p>
      {error ? (
        <p role="alert" className="mt-3 text-intent-critical-text">
          {error}
        </p>
      ) : null}
    </Dialog>
  );
}

function JobDetail({ job, onChanged }: { job: UpdateJob; onChanged: () => void }) {
  const { retryUpdate, cancelUpdate } = useFrigateUpdateActions();
  const { keyFor, reset } = useTicketOperationKey();
  const [confirming, setConfirming] = useState<Confirming>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const inFlight = useRef(false);

  const close = useCallback(() => {
    setConfirming(undefined);
    setError(undefined);
    reset();
  }, [reset]);

  const confirm = useCallback(async () => {
    if (inFlight.current || !confirming) return;
    inFlight.current = true;
    setBusy(true);
    setError(undefined);
    try {
      // The key lives as long as the confirmation, so resubmitting the same
      // action after a failure reuses it.
      const key = keyFor({ action: confirming, jobId: job.jobId });
      let message = "Frigate update retry requested.";
      if (confirming === "cancel") {
        // A cancel can be accepted while Start is still in flight, or queued
        // for Frigate's driver. Only a cancelled job has really stopped.
        const updated = await cancelUpdate(job.jobId, key);
        message =
          updated?.state === FrigateUpdateState.CANCELLED
            ? "Frigate update cancelled."
            : "Cancel requested. The update stops only if no controller has stopped yet.";
      } else {
        await retryUpdate(job.jobId, key);
      }
      pushToast({ message, status: STATUSES.success });
      close();
      onChanged();
    } catch (cause) {
      setError(
        getErrorMessage(cause, confirming === "retry" ? "Couldn't retry the update." : "Couldn't cancel the update."),
      );
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }, [cancelUpdate, close, confirming, job.jobId, keyFor, onChanged, retryUpdate]);

  const phase = frigateRolloutPhaseLabel(job.rolloutPhase);
  const failure = job.errorCode || job.errorMessage ? describeFrigateError(job.errorCode, job.errorMessage) : undefined;
  const unreachableSince = timestampSeconds(job.unreachableSince);
  const stateLine = [
    frigateStateLabel(job.state),
    phase && job.state === FrigateUpdateState.RUNNING ? phase : undefined,
    job.cancelRequestedAt && job.state === FrigateUpdateState.STARTING ? "cancel requested" : undefined,
  ]
    .filter(Boolean)
    .join(": ");

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="text-emphasis-300 text-text-primary" data-testid="frigate-update-state">
            {stateLine}
          </div>
          <div className="text-200 text-text-primary-70">
            {job.fromVersion ? `${job.fromVersion} to ${job.targetVersion}` : `To ${job.targetVersion}`}
            {job.stageOnly ? ", package check only" : ""}
          </div>
        </div>
        <div className="flex gap-2">
          {job.retryable ? (
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Retry"
              onClick={() => setConfirming("retry")}
            />
          ) : null}
          {job.cancellable ? (
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Cancel update"
              onClick={() => setConfirming("cancel")}
            />
          ) : null}
        </div>
      </div>
      {job.servedByMaintenance ? (
        <Callout
          intent="information"
          prefixIcon={<Info />}
          title="Frigate is stopped for the install."
          subtitle="The maintenance service is answering for this container. Status keeps updating, and Frigate restarts on its own."
          testId="frigate-update-maintenance"
        />
      ) : null}
      {unreachableSince ? (
        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title="Fleet can't reach either controller."
          subtitle={`No answer since ${formatTimestamp(unreachableSince, { includeSeconds: true })}. The update continues on the controllers.`}
          testId="frigate-update-unreachable"
        />
      ) : null}
      {failure ? (
        <Callout
          intent="danger"
          prefixIcon={<Alert />}
          title={failure.title}
          subtitle={`${failure.detail ? `${failure.detail} ` : ""}Code: ${failure.code || "unknown"}`}
          testId="frigate-update-error"
        />
      ) : null}
      {job.nextStep ? (
        <p className="text-300 text-text-primary" data-testid="frigate-update-next-step">
          <span className="text-emphasis-300">Next step: </span>
          {job.nextStep}
        </p>
      ) : null}
      {job.instances.length > 0 ? (
        <ul aria-label="Controller progress" className="flex flex-col gap-2">
          {job.instances.map((instance) => (
            <InstanceRow
              key={instance.instanceId}
              instance={instance}
              driver={instance.instanceId === job.driverInstanceId}
            />
          ))}
        </ul>
      ) : null}
      {failure || job.retryable ? (
        <a
          href={FRIGATE_UPDATE_RUNBOOK_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="text-300 text-core-accent-fill underline"
        >
          Frigate update runbook
        </a>
      ) : null}
      {confirming ? (
        <ConfirmDialog kind={confirming} busy={busy} error={error} onConfirm={() => void confirm()} onDismiss={close} />
      ) : null}
    </div>
  );
}

export default function FrigateUpdatePanel({
  jobs,
  loading,
  error,
  onChanged,
}: {
  jobs: UpdateJob[];
  loading: boolean;
  error?: string;
  onChanged: () => void;
}) {
  const [latest, ...earlier] = jobs;
  if (loading && !latest) return null;
  if (!latest && !error) return null;
  return (
    <section
      aria-label="Frigate update"
      className="flex flex-col gap-4 rounded-xl bg-core-primary-5 p-6"
      data-testid="frigate-update-panel"
    >
      <h2 className="text-heading-200 text-text-primary">Frigate update</h2>
      {error ? (
        <p role="alert" className="text-300 text-intent-critical-text">
          {error}
        </p>
      ) : null}
      {latest ? <JobDetail key={latest.jobId} job={latest} onChanged={onChanged} /> : null}
      {earlier.length > 0 ? (
        <div className="flex flex-col gap-2">
          <h3 className="text-emphasis-300 text-text-primary">Recent updates</h3>
          <ul aria-label="Recent Frigate updates" className="flex flex-col divide-y divide-border-5">
            {earlier.map((job) => (
              <li key={job.jobId} className="flex flex-wrap items-baseline justify-between gap-2 py-2 text-300">
                <span className="text-text-primary">
                  {job.fromVersion ? `${job.fromVersion} to ${job.targetVersion}` : `To ${job.targetVersion}`}
                  {isFrigateUpdateActive(job) ? " (in progress)" : ""}
                </span>
                <span className="text-text-primary-70">
                  {frigateStateLabel(job.state)}
                  {job.createdAt ? `, ${formatTimestamp(timestampSeconds(job.createdAt))}` : ""}
                </span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </section>
  );
}
