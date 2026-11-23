import { useState } from "react";
import type {
  FrigateRollout,
  FrigateRolloutPhase,
  FrigateServerUpdate,
  FrigateUpdateRecord,
} from "@/protoOS/api/adapters/frigateServerUpdate";
import { describeFrigateUpdateProblem, type FrigateUpdateMessage } from "@/protoOS/api/adapters/frigateUpdateMessages";
import { type FrigateUpdateActivity, useFrigateServerUpdate } from "@/protoOS/api/hooks/useFrigateServerUpdate";
import { Alert } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import { ButtonProps } from "@/shared/components/ButtonGroup/types";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import ReleaseFilePicker from "@/shared/features/frigateUpdate/ReleaseFilePicker";
import { RELEASE_PACKAGE_FILE_KINDS } from "@/shared/features/frigateUpdate/releaseFiles";

type UpdateState = ReturnType<typeof useFrigateServerUpdate>;

const phaseText: Record<FrigateRolloutPhase, string> = {
  verifying: "Verifying the package",
  stopping: "Stopping controller software",
  installing: "Installing",
  migrating: "Migrating the database",
  starting: "Starting controller software",
  succeeded: "Update complete",
  failed: "Update failed",
  cancelled: "Update cancelled",
  verified: "Package verified, nothing was stopped",
  abandoned: "Abandoned on the controller",
  unknown: "Checking update status…",
};

const activityText: Record<FrigateUpdateActivity, string> = {
  checking: "Checking the controllers…",
  reading: "Reading the release…",
  staging: "Copying the package to the controllers",
  starting: "Starting the update…",
  retrying: "Retrying the update…",
  cancelling: "Cancelling the update…",
  discarding: "Discarding the copied package…",
};

/** What the dialog is doing right now, if anything. */
const progressText = ({ activity, stagePercent }: UpdateState) =>
  activity === "staging"
    ? `${activityText.staging}: ${stagePercent}%. Keep this page open.`
    : activity && activityText[activity];

const time = (value: Date) => value.toLocaleTimeString();

/** A blocker, refusal or failure. It stays beside the action it explains until that state changes. */
function Problem({ message }: { message: FrigateUpdateMessage }) {
  return (
    <div role="alert">
      <Callout
        intent="danger"
        prefixIcon={<Alert />}
        title={message.title}
        subtitle={
          message.detail || message.code ? (
            <>
              {message.detail ? <span className="block">{message.detail}</span> : null}
              {message.code ? <span className="block text-200">{message.code}</span> : null}
            </>
          ) : undefined
        }
      />
    </div>
  );
}

function Problems({ messages }: { messages: FrigateUpdateMessage[] }) {
  return messages.map((message) => <Problem key={message.title} message={message} />);
}

function ControllerProgress({ rollout }: { rollout: FrigateRollout }) {
  return (
    <ul className="space-y-3" aria-label="Controller update progress">
      {rollout.instances.map((instance) => (
        <li key={instance.instanceId}>
          <p>{instance.instanceId}</p>
          <p className="text-text-primary-70">
            {[
              instance.phase,
              instance.runningVersion && `version ${instance.runningVersion}`,
              instance.lastContact && `last report ${time(instance.lastContact)}`,
              instance.contactStale && "no recent report",
            ]
              .filter(Boolean)
              .join(" · ")}
          </p>
          {instance.problem ? (
            <p className="text-text-critical">
              {describeFrigateUpdateProblem(instance.problem.code, instance.problem.message).title}
            </p>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

/** An update the controllers have, or one whose Start is being confirmed. */
function RolloutProgress({ state }: { state: UpdateState }) {
  const { rollout, record } = state;
  const waiting = state.lost
    ? "The controllers no longer have a record of this update."
    : "Checking whether the update started…";
  return (
    <>
      <p>
        Controller software {rollout?.fromVersion || "—"} → {rollout?.targetVersion || record?.targetVersion || "—"}
      </p>
      <p role="status">{progressText(state) ?? (rollout ? phaseText[rollout.phase] : waiting)}</p>
      {state.servedByMaintenance ? (
        <p>
          The controller software is stopped for the update, and the controller's maintenance service is answering. Keep
          this page open: it can't load again until the controller software restarts.
        </p>
      ) : null}
      {state.unanswered ? (
        <p role="status" className="text-text-primary-70">
          The controller isn't answering ({state.unanswered}). The controller software restarts during the update, so
          this can take a few minutes.
          {state.lastAnswer ? ` Last answer at ${time(state.lastAnswer)}.` : ""}
        </p>
      ) : null}
      {rollout?.problem ? (
        <Problem message={describeFrigateUpdateProblem(rollout.problem.code, rollout.problem.message)} />
      ) : null}
      {rollout?.nextStep ? <p>{rollout.nextStep}</p> : null}
      <Problems messages={state.alerts} />
      {rollout?.instances.length ? <ControllerProgress rollout={rollout} /> : null}
      {rollout ? (
        <p className="text-200 break-all text-text-primary-70">
          {[
            rollout.actor && `Started by ${rollout.actor}`,
            rollout.releaseKeyId && `Signed by key ${rollout.releaseKeyId}`,
          ]
            .filter(Boolean)
            .join(" · ")}
        </p>
      ) : null}
    </>
  );
}

/** A package this browser copied to the controllers that has not started. */
function CopiedPackage({
  state,
  record,
  resumable,
}: {
  state: UpdateState;
  record: FrigateUpdateRecord;
  resumable: boolean;
}) {
  const interrupted = resumable
    ? "Copying the package was interrupted. Copy it again, or discard it."
    : "Copying the package was interrupted. Discard it, then choose the release again.";
  return (
    <>
      <p className="break-all">
        Version {record.targetVersion} · {record.packageName}
      </p>
      <p role="status">
        {progressText(state) ??
          (record.step === "staged"
            ? "The package is copied to the controllers but the update hasn't started."
            : interrupted)}
      </p>
      {record.notStarted ? <Problem message={record.notStarted} /> : null}
      <Problems messages={state.alerts} />
    </>
  );
}

/** Choosing a release and seeing what it would change on each controller. */
function ReleaseChoice({ state }: { state: UpdateState }) {
  const { pkg, plan } = state;
  const progress = progressText(state);
  return (
    <>
      <p>
        The controller software stops and restarts on each controller during the update. The controllers verify the
        release signature before anything stops.
      </p>
      <ReleaseFilePicker
        kinds={RELEASE_PACKAGE_FILE_KINDS}
        files={state.files}
        onChange={(files) => void state.choose(files)}
        onReject={state.reject}
        emptyHint="Select the .deb, release statement and signature."
        inputLabel="Controller software release files"
        disabled={state.busy}
      />
      {pkg ? (
        <div className="break-all">
          <p>
            Version {pkg.version} · {pkg.architecture}
            {pkg.issuedAt ? ` · released ${pkg.issuedAt.slice(0, 10)}` : ""}
          </p>
          <p className="text-200 text-text-primary-70">SHA-256 {pkg.sha256}</p>
        </div>
      ) : null}
      {plan?.controllers.length ? (
        <ul className="space-y-1" aria-label="Controllers">
          {plan.controllers.map((controller) => (
            <li key={controller.instanceId} className="flex justify-between gap-4">
              <span>{controller.instanceId}</span>
              <span className="text-text-primary-70">
                Version {controller.runningVersion || "—"}
                {pkg ? ` → ${pkg.version}` : ""}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {progress ? <p role="status">{progress}</p> : null}
      <Problems messages={(plan?.blockers ?? []).map((title) => ({ title }))} />
      <Problems messages={state.alerts} />
    </>
  );
}

export default function ContainerFrigateSoftwareUpdate({
  update,
  deviceId,
  duringDowntime = false,
}: {
  update: FrigateServerUpdate;
  deviceId: string;
  /** Shown in place of the app while Frigate isn't answering: only an update being followed, with its dialog open. */
  duringDowntime?: boolean;
}) {
  const state = useFrigateServerUpdate(update, deviceId);
  const [open, setOpen] = useState(duringDowntime);
  const [confirmingRelease, setConfirmingRelease] = useState(false);
  const { record, rollout, pkg, plan } = state;
  const close = () => {
    if (!state.busy) setOpen(false);
  };
  const tracking = !!state.following;
  const title = tracking || record ? "Controller software update" : "Update controller software";
  const ready = !!pkg && !!plan && !plan.blockers.length && !record;
  const resumable = record?.step === "staging" && pkg?.version === record.targetVersion;
  const releasing = confirmingRelease && (tracking || !!record);

  const buttons: ButtonProps[] = [{ text: "Close", variant: "secondary", disabled: state.busy, onClick: close }];
  if (state.activity === "staging") buttons.push({ text: "Stop copying", variant: "primary", onClick: state.abort });
  else if (tracking) {
    if (rollout?.ended && !rollout.retryable)
      buttons.push({
        text: "Done",
        variant: "primary",
        onClick: () => {
          state.release();
          setOpen(false);
        },
      });
    else {
      // Releasing changes nothing on the controllers, so it is offered whenever the update can't be followed. A failure
      // that can be retried is always shown: Frigate may stay stopped until it is retried or cancelled. While the app
      // waits for Frigate it isn't offered: an unanswered poll is expected then, and nothing else leads back here.
      if (!duringDowntime && (state.lost || state.unanswered))
        buttons.push({
          text: "Stop tracking",
          variant: "secondary",
          disabled: state.busy,
          onClick: () => setConfirmingRelease(true),
        });
      if (rollout?.cancellable)
        buttons.push({ text: "Cancel update", variant: "secondary", disabled: state.busy, onClick: state.cancel });
      if (rollout?.retryable)
        buttons.push({ text: "Retry", variant: "primary", disabled: state.busy, onClick: state.retry });
    }
  } else if (record) {
    // A controller that stays unreachable can never confirm the discard, and the copy would block every later update.
    if (state.discardFailed)
      buttons.push({
        text: "Stop tracking",
        variant: "secondary",
        disabled: state.busy,
        onClick: () => setConfirmingRelease(true),
      });
    buttons.push({ text: "Discard", variant: "secondary", disabled: state.busy, onClick: state.discard });
    if (record.step === "staged")
      buttons.push({ text: "Start update", variant: "primary", disabled: state.busy, onClick: state.start });
    else if (resumable)
      buttons.push({ text: "Copy again", variant: "primary", disabled: state.busy, onClick: state.install });
  } else buttons.push({ text: "Update", variant: "primary", disabled: state.busy || !ready, onClick: state.install });

  if (duringDowntime && !tracking) return null;
  return (
    <div className="mt-6 flex justify-center">
      <Button
        variant="secondary"
        size="compact"
        onClick={() => {
          setOpen(true);
          if (!record && !tracking) void state.check();
        }}
      >
        {tracking || record ? "Controller software update status" : "Update controller software"}
      </Button>
      {open ? (
        <Dialog title={title} manageFocus inert={releasing} onDismiss={close} buttons={buttons}>
          <div className="space-y-4 text-300">
            {tracking ? (
              <RolloutProgress state={state} />
            ) : record ? (
              <CopiedPackage state={state} record={record} resumable={resumable} />
            ) : (
              <ReleaseChoice state={state} />
            )}
          </div>
        </Dialog>
      ) : null}
      {open && releasing ? (
        <Dialog
          title="Stop tracking this update?"
          subtitle={
            tracking
              ? "This browser will no longer show it. Nothing changes on the controllers."
              : "This browser will no longer show it. The copied package may stay on the controllers, and it can't be discarded from here afterwards."
          }
          subtitleSize="text-300"
          manageFocus
          onDismiss={() => setConfirmingRelease(false)}
          buttons={[
            { text: "Keep tracking", variant: "secondary", onClick: () => setConfirmingRelease(false) },
            {
              text: "Stop tracking",
              variant: "primary",
              onClick: () => {
                state.release();
                setConfirmingRelease(false);
              },
            },
          ]}
        />
      ) : null}
    </div>
  );
}
