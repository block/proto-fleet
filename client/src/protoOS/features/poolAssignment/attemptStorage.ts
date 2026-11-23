import { poolRecoveryFingerprint, recoveryPoolURL } from "@/protoOS/api/adapters/mdkV2/poolRecoveryEvidence";
import type { PoolAssignmentAttempt } from "@/protoOS/api/adapters/poolAssignment";

const prefix = (controller: string) => `proto-os:pool-assignment:${encodeURIComponent(controller)}:`;
const ATTEMPTS_CHANGED = "proto-os-pool-attempts-changed";

export const subscribeToPoolAttempts = (controller: string, onChange: () => void) => {
  const storagePrefix = prefix(controller);
  const onStorage = (event: StorageEvent) => {
    if (event.storageArea === localStorage && (event.key === null || event.key.startsWith(storagePrefix))) onChange();
  };
  const onLocalChange = (event: Event) => {
    if ((event as CustomEvent<string>).detail === controller) onChange();
  };
  window.addEventListener("storage", onStorage);
  window.addEventListener(ATTEMPTS_CHANGED, onLocalChange);
  return () => {
    window.removeEventListener("storage", onStorage);
    window.removeEventListener(ATTEMPTS_CHANGED, onLocalChange);
  };
};

const notifyPoolAttempts = (controller: string) =>
  window.dispatchEvent(new CustomEvent(ATTEMPTS_CHANGED, { detail: controller }));

const isPoolAttempt = (value: unknown, key: string, storagePrefix: string): value is PoolAssignmentAttempt => {
  if (!value || typeof value !== "object") return false;
  const attempt = value as Partial<PoolAssignmentAttempt>;
  return Boolean(
    attempt.id &&
    key === storagePrefix + attempt.id &&
    Array.isArray(attempt.macs) &&
    (attempt.macs.length || attempt.configurationOnly === true) &&
    attempt.macs.every((mac) => typeof mac === "string") &&
    typeof attempt.destinationRef === "string" &&
    typeof attempt.phase === "string" &&
    typeof attempt.configurationConfirmed === "boolean" &&
    (attempt.unassignedOnly === undefined || typeof attempt.unassignedOnly === "boolean") &&
    (attempt.restoreLegacyMining === undefined || typeof attempt.restoreLegacyMining === "boolean") &&
    (attempt.unsubmittedWorkerNameBoards === undefined ||
      (Number.isSafeInteger(attempt.unsubmittedWorkerNameBoards) && attempt.unsubmittedWorkerNameBoards >= 0)) &&
    Array.isArray(attempt.pools) &&
    Array.isArray(attempt.operationIds),
  );
};

const safePoolEvidence = ({ pools, poolFingerprint }: PoolAssignmentAttempt) => {
  const publicPools = pools.map(({ id, name, url, username, priority, enabled, shareWeight }) => ({
    id,
    name,
    url: recoveryPoolURL(url),
    username,
    priority,
    enabled,
    shareWeight,
  }));
  const redacted = publicPools.some(({ url }, index) => url !== pools[index].url);
  return {
    pools: publicPools,
    poolFingerprint: poolFingerprint ?? (redacted ? poolRecoveryFingerprint(pools) : undefined),
  };
};

export function readPoolAttempts(controller: string): PoolAssignmentAttempt[] {
  const records: PoolAssignmentAttempt[] = [];
  const storagePrefix = prefix(controller);
  try {
    for (let index = 0; index < localStorage.length; index++) {
      const key = localStorage.key(index);
      if (!key?.startsWith(storagePrefix)) continue;
      try {
        const value: unknown = JSON.parse(localStorage.getItem(key) || "null");
        if (isPoolAttempt(value, key, storagePrefix)) {
          if (value.pools.some(({ url }) => recoveryPoolURL(url) !== url)) {
            const sanitized = { ...value, ...safePoolEvidence(value) };
            records.push(sanitized);
            localStorage.setItem(key, JSON.stringify(sanitized));
          } else records.push(value);
        }
      } catch {
        /* Ignore a damaged record without hiding the other attempts. */
      }
    }
  } catch {
    /* Saving must succeed before the first write. */
  }
  return records.sort((a, b) => a.id.localeCompare(b.id));
}
export const savePoolAttempt = (controller: string, attempt: PoolAssignmentAttempt) => {
  // Pick fields explicitly, even if a caller accidentally passes a plan with credentials.
  const {
    id,
    name,
    macs,
    unassignedOnly,
    destinationId,
    destinationRevision,
    destinationDefaultPoolRevision,
    unconfirmedTransferRevision,
    destinationRef,
    configurationOnly,
    configurationConfirmed,
    operationIds,
    phase,
    restoreLegacyMining,
    unsubmittedWorkerNameBoards,
  } = attempt;
  localStorage.setItem(
    prefix(controller) + id,
    JSON.stringify({
      id,
      name,
      macs,
      unassignedOnly,
      destinationId,
      destinationRevision,
      destinationDefaultPoolRevision,
      unconfirmedTransferRevision,
      destinationRef,
      configurationOnly,
      configurationConfirmed,
      ...safePoolEvidence(attempt),
      operationIds,
      phase,
      restoreLegacyMining,
      unsubmittedWorkerNameBoards,
    }),
  );
  notifyPoolAttempts(controller);
};
export const removePoolAttempt = (controller: string, id: string) => {
  localStorage.removeItem(prefix(controller) + id);
  notifyPoolAttempts(controller);
};
