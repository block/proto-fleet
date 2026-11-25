import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  FrigateAgentPhase,
  FrigateInstallationType,
  FrigatePackageKind,
  FrigateRolloutPhase,
  FrigateUpdateState,
  InstanceStageState,
  type InstanceSupport,
  ProtoOSBundleSource,
  type ProtoOSJob,
  ProtoOSJobState,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { ETCD_UNHEALTHY, FRIGATE_UPDATE_MESSAGES_BY_CODE } from "@/shared/features/frigateUpdate/codeText";

export const FRIGATE_UPDATE_PERMISSION = "controller:software_update";

// The runbook location is not final; change it here and nowhere else.
export const FRIGATE_UPDATE_RUNBOOK_URL =
  "https://github.com/block/proto-fleet/blob/main/docs/development/frigate-server-updates.md";

const REMOTE_TRIGGER_DISABLED =
  "Remote updates are turned off on a controller. On that controller, set update.allow_remote_trigger to true in its configuration, then check again.";
const HELPER_UNAVAILABLE =
  "A controller's update helper is not available. Install the controller software package that includes the update helper and make sure the helper service is running.";
const SELF_COORDINATION_UNSUPPORTED =
  "A controller's software version can't coordinate its own update. Update it once by hand to a version that supports Fleet updates.";
const UNSUPPORTED_INSTALLATION =
  "A controller wasn't installed from a Debian package, so it can't be updated from Fleet.";
const CONTROLLER_UNREACHABLE =
  "Fleet can't reach a controller. Check its network connection and that its software is running.";
const PROTOOS_UNSUPPORTED =
  "A controller doesn't accept ProtoOS updates from Fleet. Update its controller software first.";
const UPDATE_RUNNING = "An update is already running on this controller.";

// Codes are an open set with an UPDATE_ prefix from Frigate and a FLEET_ prefix
// from Fleet, so lookups ignore the prefix. Frigate's own codes are worded once
// for ProtoOS and Fleet; the entries here are Fleet's codes and the ones whose
// next step is specific to Fleet.
const CODE_TEXT: Record<string, string> = {
  ...FRIGATE_UPDATE_MESSAGES_BY_CODE,
  REMOTE_TRIGGER_DISABLED,
  HELPER_UNAVAILABLE,
  SELF_COORDINATION_UNSUPPORTED,
  UNSUPPORTED_INSTALLATION,
  CONTROLLER_UNREACHABLE,
  INSTANCE_UNREACHABLE: CONTROLLER_UNREACHABLE,
  CONTROLLER_UNSUPPORTED: "This container's controllers can't be updated from Fleet.",
  NO_INSTANCES: "No controllers are reporting for this container.",
  CLUSTER_MISMATCH: "The two controllers report different clusters, so they can't be updated together.",
  ARCHITECTURE_MISMATCH: "The selected package was built for a different CPU architecture than a controller.",
  RELEASE_KEY_UNTRUSTED: "A controller doesn't trust the key that signed this package.",
  RELEASE_SIGNATURE_INVALID: "A controller rejected the package's release signature.",
  UPDATE_IN_PROGRESS: "Another update is already in progress for this container.",
  ALREADY_RUNNING: "Another update is already in progress for this container.",
  ACTIVE_JOB: "Another update is already in progress for this container.",
  PACKAGE_MISSING:
    "This Fleet server no longer has the package. Upload it again, then start a new update. A rollout that is already running is not affected.",
  STAGE_FAILED: "Fleet couldn't copy the package to a controller.",
  STAGE_STALLED: "Copying the package to a controller stopped making progress.",
  STAGE_DIGEST_MISMATCH: "A controller received a package that doesn't match the uploaded file.",
  ROLLOUT_NOT_FOUND: "The controllers no longer know this update.",
  FIRMWARE_ACTIVE:
    "A hashboard or PSU firmware update is running on a controller. Wait for it to finish, then start the update again.",
  PROTOOS_UNSUPPORTED,
  PROTOOS_REPOSITORY_UNTRUSTED: "A controller doesn't accept ProtoOS releases from the repository of this package.",
  PROTOOS_INSTALL_FAILED: "A controller couldn't install ProtoOS.",
  PROTOOS_JOB_NOT_REPORTED: "A controller didn't report the ProtoOS install of this update.",
  PROTOOS_INSTALL_TIMEOUT: "A controller didn't finish the ProtoOS install in time.",
};

function codeKey(code: string): string {
  return code.replace(/^(UPDATE|FLEET)_/, "");
}

/** Readable text for a known code, or undefined for a code this client doesn't know. */
export function frigateCodeText(code: string): string | undefined {
  return code ? CODE_TEXT[codeKey(code)] : undefined;
}

export interface FrigateErrorDescription {
  /** Headline: mapped text, or a generic fallback. */
  title: string;
  /** The server's own message, when it adds something the headline doesn't say. */
  detail?: string;
  /** Always shown so an unmapped failure can still be reported. */
  code: string;
}

export function describeFrigateError(code: string, message: string): FrigateErrorDescription {
  const text = frigateCodeText(code);
  if (text) return { title: text, detail: message || undefined, code };
  return { title: message || "The update failed.", code };
}

const STATE_LABELS: Partial<Record<FrigateUpdateState, string>> = {
  [FrigateUpdateState.STAGING]: "Copying package to controllers",
  [FrigateUpdateState.STAGED]: "Package staged",
  [FrigateUpdateState.STARTING]: "Starting update",
  [FrigateUpdateState.RUNNING]: "Update in progress",
  [FrigateUpdateState.SUCCEEDED]: "Update complete",
  [FrigateUpdateState.FAILED]: "Update failed",
  [FrigateUpdateState.CANCELLED]: "Cancelled",
  [FrigateUpdateState.VERIFIED]: "Package verified, nothing was stopped",
  [FrigateUpdateState.ABANDONED]: "Abandoned on the controller",
};

export const frigateStateLabel = (state: FrigateUpdateState) => STATE_LABELS[state] ?? "Unknown";

const ROLLOUT_PHASE_LABELS: Partial<Record<FrigateRolloutPhase, string>> = {
  [FrigateRolloutPhase.VERIFYING]: "Verifying",
  [FrigateRolloutPhase.STOPPING]: "Stopping controller software",
  [FrigateRolloutPhase.INSTALLING]: "Installing",
  [FrigateRolloutPhase.MIGRATING]: "Migrating the database",
  [FrigateRolloutPhase.STARTING]: "Starting controller software",
  [FrigateRolloutPhase.SUCCEEDED]: "Succeeded",
  [FrigateRolloutPhase.FAILED]: "Failed",
  [FrigateRolloutPhase.CANCELLED]: "Cancelled",
  [FrigateRolloutPhase.VERIFIED]: "Verified",
  [FrigateRolloutPhase.ABANDONED]: "Abandoned",
  [FrigateRolloutPhase.UNKNOWN]: "Unknown phase",
};

/** Empty when the rollout has not reported a phase yet. */
export const frigateRolloutPhaseLabel = (phase: FrigateRolloutPhase) => ROLLOUT_PHASE_LABELS[phase] ?? "";

const AGENT_PHASE_LABELS: Partial<Record<FrigateAgentPhase, string>> = {
  [FrigateAgentPhase.CONNECTING]: "Connecting",
  [FrigateAgentPhase.STAGED]: "Staged",
  [FrigateAgentPhase.STOPPED]: "Software stopped",
  [FrigateAgentPhase.INSTALLED]: "Installed",
  [FrigateAgentPhase.MIGRATED]: "Migrated",
  [FrigateAgentPhase.STARTED]: "Software started",
  [FrigateAgentPhase.FAILED]: "Failed",
  [FrigateAgentPhase.UNKNOWN]: "Unknown phase",
};

export const frigateAgentPhaseLabel = (phase: FrigateAgentPhase) => AGENT_PHASE_LABELS[phase] ?? "Not reported";

const STAGE_LABELS: Partial<Record<InstanceStageState, string>> = {
  [InstanceStageState.PENDING]: "Waiting to copy",
  [InstanceStageState.STAGING]: "Copying",
  [InstanceStageState.STAGED]: "Copied",
  [InstanceStageState.FAILED]: "Copy failed",
};

export const frigateStageLabel = (state: InstanceStageState) => STAGE_LABELS[state] ?? "";

export function timestampSeconds(timestamp: Timestamp | undefined): number | undefined {
  return timestamp ? Number(timestamp.seconds) : undefined;
}

/** What is wrong with one controller, from the fields GetSupport reports for it. */
export function instanceSupportProblems(instance: InstanceSupport): string[] {
  if (instance.error) return [`Fleet couldn't read this controller: ${instance.error}`];
  const problems: string[] = [];
  if (!instance.remoteTriggerAllowed) problems.push(REMOTE_TRIGGER_DISABLED);
  if (!instance.helperAvailable) problems.push(HELPER_UNAVAILABLE);
  if (!instance.selfCoordinationSupported) problems.push(SELF_COORDINATION_UNSUPPORTED);
  // Fleet accepts only Debian package installations, so an unreported type is
  // flagged too.
  if (instance.installationType !== FrigateInstallationType.DEBIAN_PACKAGE) {
    problems.push(UNSUPPORTED_INSTALLATION);
  }
  // Match the server: an update is refused only once a majority is unhealthy.
  if (instance.etcdMembersTotal > 0 && instance.etcdMembersHealthy * 2 <= instance.etcdMembersTotal) {
    problems.push(
      `${ETCD_UNHEALTHY} ${instance.etcdMembersHealthy} of ${instance.etcdMembersTotal} members are healthy.`,
    );
  }
  if (instance.activeJobId) problems.push(UPDATE_RUNNING);
  return problems;
}

const PROTO_OS_JOB_STATE_LABELS: Partial<Record<ProtoOSJobState, string>> = {
  [ProtoOSJobState.PROTO_OS_JOB_STATE_QUEUED]: "Verifying the copy",
  [ProtoOSJobState.PROTO_OS_JOB_STATE_INSTALLING]: "Installing",
  [ProtoOSJobState.PROTO_OS_JOB_STATE_SUCCEEDED]: "Installed",
  [ProtoOSJobState.PROTO_OS_JOB_STATE_FAILED]: "Install failed",
};

/** Empty while the controller reports no state for this job. */
export const protoOSJobStateLabel = (state: ProtoOSJobState) =>
  state === ProtoOSJobState.PROTO_OS_JOB_STATE_UNSPECIFIED ? "" : (PROTO_OS_JOB_STATE_LABELS[state] ?? "Unknown state");

// Match the server: a state this client doesn't know is not final.
function protoOSJobActive(job: ProtoOSJob | undefined): boolean {
  if (!job?.jobId) return false;
  return ![
    ProtoOSJobState.PROTO_OS_JOB_STATE_UNSPECIFIED,
    ProtoOSJobState.PROTO_OS_JOB_STATE_SUCCEEDED,
    ProtoOSJobState.PROTO_OS_JOB_STATE_FAILED,
  ].includes(job.state);
}

/** One controller's readiness for an update of one package kind. */
export interface ControllerSupport {
  /** What the controller runs now. */
  summary: string;
  problems: string[];
  /** The controller's own explanation, when it gives one. */
  note?: string;
}

function controllerSoftwareSupport(instance: InstanceSupport): ControllerSupport {
  return {
    summary:
      [instance.runningVersion && `Version ${instance.runningVersion}`, instance.architecture]
        .filter(Boolean)
        .join(", ") || "Version unknown",
    problems: instanceSupportProblems(instance),
    note: instance.supportMessage || undefined,
  };
}

function protoOSServed(instance: InstanceSupport): string {
  const support = instance.protoOs;
  switch (support?.servedSource) {
    case ProtoOSBundleSource.PROTO_OS_BUNDLE_SOURCE_PACKAGE: {
      const version = support.servedReleaseTag || support.installedVersion;
      return version ? `ProtoOS ${version}` : "ProtoOS version unknown";
    }
    case ProtoOSBundleSource.PROTO_OS_BUNDLE_SOURCE_BUNDLED:
      return "ProtoOS from the controller software";
    case ProtoOSBundleSource.PROTO_OS_BUNDLE_SOURCE_NONE:
      return "No ProtoOS";
    default:
      return "ProtoOS version unknown";
  }
}

// The server checks a ProtoOS update without self coordination, etcd health,
// the architecture or the update helper, because the controller software keeps
// running. The package repository is checked against the chosen package, so
// only the server's verdict reports it.
function protoOSSupport(instance: InstanceSupport): ControllerSupport {
  const summary = protoOSServed(instance);
  if (instance.error) return { summary, problems: [`Fleet couldn't read this controller: ${instance.error}`] };
  const support = instance.protoOs;
  const problems: string[] = [];
  if (!support?.selfSupported) problems.push(PROTOOS_UNSUPPORTED);
  if (!instance.remoteTriggerAllowed) problems.push(REMOTE_TRIGGER_DISABLED);
  if (instance.activeJobId || protoOSJobActive(support?.job)) problems.push(UPDATE_RUNNING);
  return { summary, problems, note: support?.supportMessage || undefined };
}

/** How the panel shows the progress of each controller. */
export type ProgressStyle =
  // The controllers run one coordinated rollout through phases.
  | "rollout"
  // Each controller installs its copy alone as soon as the copy is committed.
  | "install";

/** What differs between the package kinds wherever a job or a package is shown. */
export interface PackageKindRules {
  /** Names the kind at the start of a sentence, in a heading or in a table cell. */
  title: string;
  /** Names the kind after a verb. */
  noun: string;
  /** The Retry confirmation. Absent when the server refuses Retry for this kind. */
  retryConfirm?: string;
  cancel: { confirm: string; pending: string };
  progress: ProgressStyle;
}

/** The rules of a kind that an operator can start an update for. */
export interface UpdatableKindRules extends PackageKindRules {
  /** What happens to the container during the update. The operator reads it before starting. */
  startWarning: { title: string; subtitle: string };
  support: (instance: InstanceSupport) => ControllerSupport;
  /**
   * Without a package, the server's verdict applies the controller software
   * rules, so for this kind it means nothing until a package is chosen.
   */
  verdictNeedsPackage: boolean;
}

export type UpdatableKind = FrigatePackageKind.CONTROLLER_SOFTWARE | FrigatePackageKind.PROTO_OS;

export const UPDATABLE_KIND_RULES: Record<UpdatableKind, UpdatableKindRules> = {
  [FrigatePackageKind.CONTROLLER_SOFTWARE]: {
    title: "Controller software",
    noun: "controller software",
    startWarning: {
      title: "Both controllers stop mining control together, briefly.",
      subtitle: "Miners keep hashing, but this container can't be managed from Fleet until both controllers restart.",
    },
    retryConfirm:
      "The update resumes from the step that failed. Both controllers stop mining control together, briefly.",
    cancel: {
      confirm:
        "No controller has been stopped, so cancelling leaves the controller software unchanged. You can start a new update afterwards.",
      pending: "Cancel requested. The update stops only if no controller has stopped yet.",
    },
    progress: "rollout",
    support: controllerSoftwareSupport,
    verdictNeedsPackage: false,
  },
  [FrigatePackageKind.PROTO_OS]: {
    title: "ProtoOS",
    noun: "ProtoOS",
    startWarning: {
      title: "An install can't be cancelled after a controller commits its copy.",
      subtitle: "The controllers and mining keep running during the update.",
    },
    cancel: {
      confirm:
        "No controller has committed its copy, so cancelling leaves ProtoOS unchanged. You can start a new update afterwards.",
      pending: "Cancel requested. The update stops only if no controller has committed its copy yet.",
    },
    progress: "install",
    support: protoOSSupport,
    verdictNeedsPackage: true,
  },
};

export const UPDATABLE_KINDS: UpdatableKind[] = [FrigatePackageKind.CONTROLLER_SOFTWARE, FrigatePackageKind.PROTO_OS];

// A kind this client doesn't know. It is never offered for an update, and no
// action is offered whose effect the client can't describe.
const OTHER_KIND_RULES: PackageKindRules = {
  title: "Other software",
  noun: "other software",
  cancel: { confirm: "You can start a new update afterwards.", pending: "Cancel requested." },
  progress: "rollout",
};

const isUpdatableKind = (kind: FrigatePackageKind): kind is UpdatableKind =>
  (UPDATABLE_KINDS as FrigatePackageKind[]).includes(kind);

export const packageKindRules = (kind: FrigatePackageKind): PackageKindRules =>
  isUpdatableKind(kind) ? UPDATABLE_KIND_RULES[kind] : OTHER_KIND_RULES;

// The REST upload responses name a kind as the server stores it.
const KIND_BY_NAME = new Map<string, FrigatePackageKind>([
  ["frigate", FrigatePackageKind.CONTROLLER_SOFTWARE],
  ["proto_os", FrigatePackageKind.PROTO_OS],
]);

export const packageKindFromName = (name: string | undefined): FrigatePackageKind =>
  KIND_BY_NAME.get(name ?? "") ?? FrigatePackageKind.UNSPECIFIED;
