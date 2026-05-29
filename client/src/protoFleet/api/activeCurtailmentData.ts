import { create as createMessage } from "@bufbuild/protobuf";
import { create as createStore } from "zustand";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  CurtailmentEventState,
  GetActiveCurtailmentRequestSchema,
  type CurtailmentEvent as ProtoCurtailmentEvent,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted } from "@/protoFleet/api/requestErrors";

export interface ActiveCurtailmentSnapshot {
  event: ProtoCurtailmentEvent | undefined;
}

export interface RefreshActiveCurtailmentOptions {
  signal?: AbortSignal;
}

export interface PendingActiveCurtailmentRefresh extends ActiveCurtailmentSnapshot {
  commit: () => ActiveCurtailmentSnapshot;
}

interface ActiveCurtailmentDataStore extends ActiveCurtailmentSnapshot {
  version: number;
}

interface InFlightActiveCurtailmentRequest {
  abortController: AbortController;
  promise: Promise<ActiveCurtailmentSnapshot>;
  settled: boolean;
  subscribers: number;
}

const initialSnapshot: ActiveCurtailmentSnapshot = { event: undefined };
const initialStoreState: ActiveCurtailmentDataStore = {
  ...initialSnapshot,
  version: 0,
};

const useActiveCurtailmentDataStore = createStore<ActiveCurtailmentDataStore>(() => initialStoreState);

let nextWriteVersion = 0;
let appliedWriteVersion = 0;
let inFlightActiveCurtailmentRequest: InFlightActiveCurtailmentRequest | null = null;
let dismissedEventUuid: string | null = null;

function getNextWriteVersion(): number {
  nextWriteVersion += 1;
  return nextWriteVersion;
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

  appliedWriteVersion = writeVersion;
  useActiveCurtailmentDataStore.setState({ event: snapshot.event, version: writeVersion });
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

function shouldPreserveCurrentActiveCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return (
    event.state === CurtailmentEventState.RESTORING ||
    event.state === CurtailmentEventState.COMPLETED ||
    event.state === CurtailmentEventState.COMPLETED_WITH_FAILURES
  );
}

function getActiveCurtailmentSnapshotFromResponse(event?: ProtoCurtailmentEvent): ActiveCurtailmentSnapshot {
  if (event) {
    return { event };
  }

  const currentSnapshot = getActiveCurtailmentSnapshot();
  return currentSnapshot.event && shouldPreserveCurrentActiveCurtailmentEvent(currentSnapshot.event)
    ? currentSnapshot
    : initialSnapshot;
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
    promise: curtailmentClient
      .getActiveCurtailment(createMessage(GetActiveCurtailmentRequestSchema, {}), { signal: abortController.signal })
      .then((response) => getActiveCurtailmentSnapshotFromResponse(response.event))
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

async function requestActiveCurtailmentSnapshot(signal?: AbortSignal): Promise<ActiveCurtailmentSnapshot> {
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
    return snapshot;
  } finally {
    signal?.removeEventListener("abort", handleAbort);
    releaseSubscriber();
  }
}

export async function fetchActiveCurtailmentData({
  signal,
}: RefreshActiveCurtailmentOptions = {}): Promise<PendingActiveCurtailmentRefresh> {
  assertNotAborted(signal);
  const writeVersion = getNextWriteVersion();
  const snapshot = await requestActiveCurtailmentSnapshot(signal);
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
  nextWriteVersion = 0;
  appliedWriteVersion = 0;
  useActiveCurtailmentDataStore.setState(initialStoreState, true);
}
