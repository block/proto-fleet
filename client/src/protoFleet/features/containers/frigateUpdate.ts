import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  FrigateAgentPhase,
  FrigateInstallationType,
  FrigateRolloutPhase,
  FrigateUpdateState,
  InstanceStageState,
  type InstanceSupport,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";

export const FRIGATE_UPDATE_PERMISSION = "controller:software_update";

// The runbook location is not final; change it here and nowhere else.
export const FRIGATE_UPDATE_RUNBOOK_URL =
  "https://github.com/btc-mining/proto-fleet-internal/blob/main/docs/development/frigate-server-updates.md";

const REMOTE_TRIGGER_DISABLED =
  "Remote updates are turned off on a controller. On that controller, set update.allow_remote_trigger to true in the Frigate configuration, then check again.";
const HELPER_UNAVAILABLE =
  "A controller's update helper is not available. Install the Frigate Debian package that includes the update helper and make sure the helper service is running.";
const SELF_COORDINATION_UNSUPPORTED =
  "A controller's Frigate version can't coordinate its own update. Update it once by hand to a version that supports Fleet updates.";
const UNSUPPORTED_INSTALLATION =
  "A controller wasn't installed from a Debian package, so it can't be updated from Fleet.";
const ETCD_UNHEALTHY = "The controllers' etcd cluster is unhealthy. Restore quorum before updating.";
const CONTROLLER_UNREACHABLE =
  "Fleet can't reach a controller. Check its network connection and that Frigate is running.";

// Codes are an open set with an UPDATE_ prefix from Frigate and a FLEET_ prefix
// from Fleet, so lookups ignore the prefix.
const CODE_TEXT: Record<string, string> = {
  REMOTE_TRIGGER_DISABLED,
  HELPER_UNAVAILABLE,
  SELF_COORDINATION_UNSUPPORTED,
  UNSUPPORTED_INSTALLATION,
  ETCD_UNHEALTHY,
  ETCD_UNAVAILABLE: ETCD_UNHEALTHY,
  CONTROLLER_UNREACHABLE,
  INSTANCE_UNREACHABLE: CONTROLLER_UNREACHABLE,
  CONTROLLER_UNSUPPORTED: "This container's controllers can't be updated from Fleet.",
  NO_INSTANCES: "No controllers are reporting for this container.",
  CLUSTER_MISMATCH: "The two controllers report different clusters, so they can't be updated together.",
  ARCHITECTURE_MISMATCH: "The selected package was built for a different CPU architecture than a controller.",
  INSUFFICIENT_SPACE: "A controller doesn't have enough free disk space for the package.",
  RELEASE_KEY_UNTRUSTED: "A controller doesn't trust the key that signed this package.",
  NO_TRUSTED_KEYS: "A controller has no trusted release keys.",
  SIGNATURE_INVALID: "A controller rejected the package's release signature.",
  RELEASE_SIGNATURE_INVALID: "A controller rejected the package's release signature.",
  UPDATE_IN_PROGRESS: "Another update is already in progress for this container.",
  ALREADY_RUNNING: "Another update is already in progress for this container.",
  ACTIVE_JOB: "Another update is already in progress for this container.",
  PACKAGE_MISSING:
    "This Fleet server no longer has the package. Upload it again, then start a new update. A rollout that is already running is not affected.",
  STAGE_FAILED: "Fleet couldn't copy the package to a controller.",
  STAGE_STALLED: "Copying the package to a controller stopped making progress.",
  STAGE_DIGEST_MISMATCH: "A controller received a package that doesn't match the uploaded file.",
  PACKAGE_REJECTED: "A controller rejected the package.",
  MANIFEST_INVALID: "A controller rejected the update request.",
  INSTALL_FAILED: "Installing the package failed on a controller.",
  MIGRATE_FAILED: "The database migration failed after the package was installed.",
  START_FAILED: "Frigate didn't start after the install.",
  HELPER_CONTACT_LOST: "A controller's update helper stopped reporting.",
  ROLLOUT_NOT_FOUND: "The controllers no longer know this update.",
  NOT_RETRYABLE: "This update can't be retried.",
  NOT_CANCELLABLE: "This update can no longer be cancelled.",
  FIRMWARE_ACTIVE:
    "A hashboard or PSU firmware update is running on a controller. Wait for it to finish, then start the update again.",
  COOLDOWN: "A controller was updated recently and is still in its cool-down period. Try again later.",
  OUTSIDE_WINDOW: "A controller only accepts updates during its maintenance window.",
  NOT_STAGED: "The package isn't staged on every controller yet.",
  STAGE_MISMATCH: "A controller has a different package staged than this update expects.",
  EXPIRED: "The update expired before it started, so nothing was stopped.",
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
  [FrigateRolloutPhase.STOPPING]: "Stopping Frigate",
  [FrigateRolloutPhase.INSTALLING]: "Installing",
  [FrigateRolloutPhase.MIGRATING]: "Migrating the database",
  [FrigateRolloutPhase.STARTING]: "Starting Frigate",
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
  [FrigateAgentPhase.STOPPED]: "Frigate stopped",
  [FrigateAgentPhase.INSTALLED]: "Installed",
  [FrigateAgentPhase.MIGRATED]: "Migrated",
  [FrigateAgentPhase.STARTED]: "Frigate started",
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
  if (instance.activeJobId) problems.push("An update is already running on this controller.");
  return problems;
}
