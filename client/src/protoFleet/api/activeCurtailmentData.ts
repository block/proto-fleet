import { create as createMessage, equals } from "@bufbuild/protobuf";
import { create as createStore } from "zustand";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  CurtailmentEventSchema,
  CurtailmentEventState,
  GetActiveCurtailmentRequestSchema,
  type CurtailmentEvent as ProtoCurtailmentEvent,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError } from "@/protoFleet/api/requestErrors";

export interface ActiveCurtailmentSnapshot {
  event: ProtoCurtailmentEvent | undefined;
}

export interface RefreshActiveCurtailmentOptions {
  signal?: AbortSignal;
}

export interface PendingActiveCurtailmentRefresh extends ActiveCurtailmentSnapshot {
  commit: () => ActiveCurtailmentSnapshot;
}

interface InFlightActiveCurtailmentRequest {
  abortController: AbortController;
  promise: Promise<ActiveCurtailmentSnapshot>;
  settled: boolean;
  subscribers: number;
  writeVersion: number;
}

interface ActiveCurtailmentRequestSnapshot {
  snapshot: ActiveCurtailmentSnapshot;
  writeVersion: number;
}

const initialSnapshot: ActiveCurtailmentSnapshot = { event: undefined };

const useActiveCurtailmentDataStore = createStore<ActiveCurtailmentSnapshot>(() => initialSnapshot);

let nextWriteVersion = 0;
let appliedWriteVersion = 0;
let inFlightActiveCurtailmentRequest: InFlightActiveCurtailmentRequest | null = null;
let dismissedEventUuid: string | null = null;
let emptyActiveRefreshCount = 0;

const preservedEmptyActiveRefreshLimit = 1;

function getNextWriteVersion(): number {
  nextWriteVersion += 1;
  return nextWriteVersion;
}

function areActiveCurtailmentSnapshotsEqual(
  current: ActiveCurtailmentSnapshot,
  next: ActiveCurtailmentSnapshot,
): boolean {
  if (!current.event || !next.event) {
    return current.event === next.event;
  }

  return equals(CurtailmentEventSchema, current.event, next.event);
}

function setActiveCurtailmentSnapshot(
  snapshot: ActiveCurtailmentSnapshot,
  writeVersion = getNextWriteVersion(),
): ActiveCurtailmentSnapshot {
  if (writeVersion < appliedWriteVersion) {
    return getActiveCurtailmentSnapshot();
  }

  if (snapshot.event?.eventUuid && snapshot.event.eventUuid === dismissedEventUuid) {
    snapshot = initialSnapshot;
  } else if (snapshot.event?.eventUuid) {
    dismissedEventUuid = null;
  }

  if (!snapshot.event) {
    emptyActiveRefreshCount = 0;
  }

  appliedWriteVersion = writeVersion;
  const currentSnapshot = getActiveCurtailmentSnapshot();
  if (areActiveCurtailmentSnapshotsEqual(currentSnapshot, snapshot)) {
    return currentSnapshot;
  }

  useActiveCurtailmentDataStore.setState(snapshot);
  return snapshot;
}

export function getActiveCurtailmentSnapshot(): ActiveCurtailmentSnapshot {
  const { event } = useActiveCurtailmentDataStore.getState();
  return { event };
}

export function useActiveCurtailmentEvent(): ProtoCurtailmentEvent | undefined {
  return useActiveCurtailmentDataStore((state) => state.event);
}

export function applyActiveCurtailmentEvent(event?: ProtoCurtailmentEvent): ActiveCurtailmentSnapshot {
  return setActiveCurtailmentSnapshot({ event });
}

export function dismissActiveCurtailmentEvent(eventUuid?: string | null): ActiveCurtailmentSnapshot {
  dismissedEventUuid = eventUuid ?? getActiveCurtailmentSnapshot().event?.eventUuid ?? null;
  return setActiveCurtailmentSnapshot(initialSnapshot);
}

function shouldPreserveTerminalActiveCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return (
    event.state === CurtailmentEventState.COMPLETED ||
    event.state === CurtailmentEventState.COMPLETED_WITH_FAILURES
  );
}

function shouldPreserveRestoringActiveCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return event.state === CurtailmentEventState.RESTORING;
}

function getActiveCurtailmentSnapshotFromResponse(event?: ProtoCurtailmentEvent): ActiveCurtailmentSnapshot {
  if (event) {
    emptyActiveRefreshCount = 0;
    return { event };
  }

  const currentSnapshot = getActiveCurtailmentSnapshot();
  if (currentSnapshot.event && shouldPreserveTerminalActiveCurtailmentEvent(currentSnapshot.event)) {
    emptyActiveRefreshCount = 0;
    return currentSnapshot;
  }

  if (
    currentSnapshot.event &&
    shouldPreserveRestoringActiveCurtailmentEvent(currentSnapshot.event) &&
    emptyActiveRefreshCount < preservedEmptyActiveRefreshLimit
  ) {
    emptyActiveRefreshCount += 1;
    return currentSnapshot;
  }

  emptyActiveRefreshCount = 0;
  return initialSnapshot;
}

function getInFlightActiveCurtailmentRequest(): InFlightActiveCurtailmentRequest {
  if (inFlightActiveCurtailmentRequest) {
    return inFlightActiveCurtailmentRequest;
  }

  const abortController = new AbortController();
  const request: InFlightActiveCurtailmentRequest = {
    abortController,
    settled: false,
    subscribers: 0,
    writeVersion: getNextWriteVersion(),
    promise: curtailmentClient
      .getActiveCurtailment(createMessage(GetActiveCurtailmentRequestSchema, {}), { signal: abortController.signal })
      .then((response) => getActiveCurtailmentSnapshotFromResponse(response.event))
      .catch((error) => {
        if (isAbortError(error, abortController.signal)) {
          throw new DOMException("The operation was aborted.", "AbortError");
        }

        throw error;
      })
      .finally(() => {
        request.settled = true;
        if (inFlightActiveCurtailmentRequest === request) {
          inFlightActiveCurtailmentRequest = null;
        }
      }),
  };

  inFlightActiveCurtailmentRequest = request;
  return request;
}

function releaseActiveCurtailmentRequestSubscriber(request: InFlightActiveCurtailmentRequest): void {
  request.subscribers = Math.max(0, request.subscribers - 1);
  if (request.subscribers === 0 && !request.settled) {
    if (inFlightActiveCurtailmentRequest === request) {
      inFlightActiveCurtailmentRequest = null;
    }
    request.abortController.abort();
  }
}

async function requestActiveCurtailmentSnapshot(signal?: AbortSignal): Promise<ActiveCurtailmentRequestSnapshot> {
  assertNotAborted(signal);

  const request = getInFlightActiveCurtailmentRequest();
  request.subscribers += 1;
  let released = false;

  const releaseSubscriber = (): void => {
    if (released) {
      return;
    }

    released = true;
    releaseActiveCurtailmentRequestSubscriber(request);
  };
  const handleAbort = (): void => releaseSubscriber();
  signal?.addEventListener("abort", handleAbort, { once: true });

  try {
    const snapshot = await request.promise;
    assertNotAborted(signal);
    return { snapshot, writeVersion: request.writeVersion };
  } finally {
    signal?.removeEventListener("abort", handleAbort);
    releaseSubscriber();
  }
}

export async function fetchActiveCurtailmentData({
  signal,
}: RefreshActiveCurtailmentOptions = {}): Promise<PendingActiveCurtailmentRefresh> {
  assertNotAborted(signal);
  const { snapshot, writeVersion } = await requestActiveCurtailmentSnapshot(signal);
  return {
    ...snapshot,
    commit: () => setActiveCurtailmentSnapshot(snapshot, writeVersion),
  };
}

export async function refreshActiveCurtailmentData(
  options: RefreshActiveCurtailmentOptions = {},
): Promise<ActiveCurtailmentSnapshot> {
  const refresh = await fetchActiveCurtailmentData(options);
  return refresh.commit();
}

export function resetActiveCurtailmentData(): void {
  inFlightActiveCurtailmentRequest?.abortController.abort();
  inFlightActiveCurtailmentRequest = null;
  dismissedEventUuid = null;
  emptyActiveRefreshCount = 0;
  appliedWriteVersion = getNextWriteVersion();
  useActiveCurtailmentDataStore.setState(initialSnapshot, true);
}
