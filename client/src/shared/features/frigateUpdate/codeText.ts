/**
 * Plain-language text for the `UPDATE_*` codes Frigate reports for a controller software update, keyed without the
 * prefix. ProtoOS and Fleet show the same text and add their own entries: codes only they produce, and the codes whose
 * next step differs between the two apps. Frigate's set is open, so a code missing here is shown with the
 * controller's own message.
 */
export const ETCD_UNHEALTHY = "The controllers' etcd cluster is unhealthy. Restore quorum before updating.";

export const FRIGATE_UPDATE_MESSAGES_BY_CODE: Record<string, string> = {
  FIRMWARE_ACTIVE:
    "A hashboard or PSU firmware update is running on a controller. Wait for it to finish, then try again.",
  ETCD_UNHEALTHY,
  ETCD_UNAVAILABLE: ETCD_UNHEALTHY,
  INSUFFICIENT_SPACE: "A controller doesn't have enough free disk space for the package.",
  SPACE_UNKNOWN: "A controller couldn't report its free disk space.",
  COOLDOWN: "These controllers were updated recently and are still in their cool-down period. Try again later.",
  OUTSIDE_WINDOW: "The controllers only accept updates during their maintenance window.",
  NO_TRUSTED_KEYS: "A controller has no trusted release keys.",
  SIGNATURE_INVALID: "A controller rejected the package's release signature.",
  PACKAGE_REJECTED: "A controller rejected the package.",
  NOT_STAGED: "The package isn't staged on every controller.",
  STAGE_MISMATCH: "A controller has a different package staged than this update expects.",
  STAGE_LIMIT: "A controller can't accept another staged package right now.",
  STAGE_UNREADABLE: "A controller couldn't read the staged package.",
  MANIFEST_INVALID: "A controller rejected the update request.",
  IDEMPOTENCY_CONFLICT: "The controllers already have a different request with this update's key.",
  STATE_CONFLICT: "The update changed while this request was being handled. Check its status, then try again.",
  NOT_FOUND: "The controllers have no record of this update.",
  NOT_RETRYABLE: "This update can't be retried.",
  NOT_CANCELLABLE: "This update can no longer be cancelled.",
  STOP_FAILED: "A controller's software didn't stop, so nothing was installed.",
  INSTALL_FAILED: "Installing the package failed on a controller.",
  MIGRATE_FAILED: "The database migration failed after the package was installed.",
  START_FAILED: "The controller software didn't start after the install.",
  STEP_TIMEOUT: "A step of the update took too long on a controller.",
  HELPER_FAILED: "A controller's update helper failed.",
  HELPER_CONTACT_LOST: "A controller's update helper stopped reporting.",
  EXPIRED: "The update expired before it started, so nothing was stopped.",
  ABANDONED: "The update was abandoned on a controller.",
};
