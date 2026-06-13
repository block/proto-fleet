import { create as createMessage, equals } from "@bufbuild/protobuf";
import { create as createStore } from "zustand";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  CurtailmentEventSchema,
  CurtailmentEventState,
  GetCurtailmentEventRequestSchema,
  ListActiveCurtailmentsRequestSchema,
  type CurtailmentEvent as ProtoCurtailmentEvent,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError } from "@/protoFleet/api/requestErrors";

export interface ActiveCurtailmentSnapshot {
  event: ProtoCurtailmentEvent | undefined;
  events: ProtoCurtailmentEvent[];
}

export interface RefreshActiveCurtailmentOptions {
  signal?: AbortSignal;
}

export interface ApplyActiveCurtailmentEventOptions {
  mergeActiveEvents?: boolean;
  preserveAgainstStaleRefresh?: boolean;
}

export interface PendingActiveCurtailmentRefresh extends ActiveCurtailmentSnapshot {
  commit: () => ActiveCurtailmentSnapshot;
}

interface InFlightActiveCurtailmentRequest {
  abortController: AbortController;
  promise: Promise<ActiveCurtailmentResponseSnapshot>;
  settled: boolean;
  subscribers: number;
  writeVersion: number;
}

interface ActiveCurtailmentRequestSnapshot {
  snapshot: ActiveCurtailmentSnapshot;
  writeVersion: number;
}

interface ActiveCurtailmentResponseSnapshot {
  snapshot: ActiveCurtailmentSnapshot;
}

interface SetActiveCurtailmentSnapshotOptions {
  fromActiveRefresh?: boolean;
  preserveAgainstStaleRefresh?: boolean;
}

const activeCurtailmentDetailTargetPageSize = 1000;

const initialSnapshot: ActiveCurtailmentSnapshot = { event: undefined, events: [] };

const useActiveCurtailmentDataStore = createStore<ActiveCurtailmentSnapshot>(() => initialSnapshot);

let nextWriteVersion = 0;
let appliedWriteVersion = 0;
let inFlightActiveCurtailmentRequest: InFlightActiveCurtailmentRequest | null = null;
let dismissedEventUuid: string | null = null;
let mutationBackedEventUuid: string | null = null;
let preservedMutationBackedRefreshWriteVersions = new Set<number>();

const preservedMutationBackedActiveRefreshLimit = 1;

function getNextWriteVersion(): number {
  nextWriteVersion += 1;
  return nextWriteVersion;
}

function areActiveCurtailmentSnapshotsEqual(
  current: ActiveCurtailmentSnapshot,
  next: ActiveCurtailmentSnapshot,
): boolean {
  if (current.events.length !== next.events.length) {
    return false;
  }

  if (!current.events.every((event, index) => equals(CurtailmentEventSchema, event, next.events[index]))) {
    return false;
  }

  if (!current.event || !next.event) {
    return current.event === next.event;
  }

  return equals(CurtailmentEventSchema, current.event, next.event);
}

function isListedActiveCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return (
    event.state === CurtailmentEventState.PENDING ||
    event.state === CurtailmentEventState.ACTIVE ||
    event.state === CurtailmentEventState.RESTORING
  );
}

function mergeActiveCurtailmentEventList(
  events: ProtoCurtailmentEvent[],
  event: ProtoCurtailmentEvent,
): ProtoCurtailmentEvent[] {
  if (!isListedActiveCurtailmentEvent(event)) {
    return events.filter((currentEvent) => currentEvent.eventUuid !== event.eventUuid);
  }

  const eventIndex = events.findIndex((currentEvent) => currentEvent.eventUuid === event.eventUuid);
  if (eventIndex === -1) {
    return [event, ...events];
  }

  return events.map((currentEvent, index) => (index === eventIndex ? event : currentEvent));
}

function removeActiveCurtailmentEventFromList(
  events: ProtoCurtailmentEvent[],
  eventUuid: string,
): ProtoCurtailmentEvent[] {
  return events.filter((event) => event.eventUuid !== eventUuid);
}

function shouldPreserveMutationBackedSnapshot(
  current: ActiveCurtailmentSnapshot,
  next: ActiveCurtailmentSnapshot,
  writeVersion: number,
): boolean {
  if (
    !mutationBackedEventUuid ||
    !current.event ||
    current.event.eventUuid !== mutationBackedEventUuid ||
    preservedMutationBackedRefreshWriteVersions.size >= preservedMutationBackedActiveRefreshLimit
  ) {
    return preservedMutationBackedRefreshWriteVersions.has(writeVersion);
  }

  if (preservedMutationBackedRefreshWriteVersions.has(writeVersion)) {
    return true;
  }

  return !next.event || !equals(CurtailmentEventSchema, current.event, next.event);
}

function clearMutationBackedPreservation(): void {
  mutationBackedEventUuid = null;
  preservedMutationBackedRefreshWriteVersions = new Set<number>();
}

function setActiveCurtailmentSnapshot(
  snapshot: ActiveCurtailmentSnapshot,
  writeVersion = getNextWriteVersion(),
  { fromActiveRefresh = false, preserveAgainstStaleRefresh = false }: SetActiveCurtailmentSnapshotOptions = {},
): ActiveCurtailmentSnapshot {
  if (writeVersion < appliedWriteVersion) {
    return getActiveCurtailmentSnapshot();
  }

  if (dismissedEventUuid) {
    snapshot = {
      event: snapshot.event?.eventUuid && snapshot.event.eventUuid === dismissedEventUuid ? undefined : snapshot.event,
      events: removeActiveCurtailmentEventFromList(snapshot.events, dismissedEventUuid),
    };
    if (snapshot.event?.eventUuid) {
      dismissedEventUuid = null;
    }
  } else if (snapshot.event?.eventUuid) {
    dismissedEventUuid = null;
  }

  const currentSnapshot = getActiveCurtailmentSnapshot();
  if (fromActiveRefresh && shouldPreserveMutationBackedSnapshot(currentSnapshot, snapshot, writeVersion)) {
    preservedMutationBackedRefreshWriteVersions.add(writeVersion);
    return currentSnapshot;
  }

  if (preserveAgainstStaleRefresh && snapshot.event) {
    mutationBackedEventUuid = snapshot.event.eventUuid;
    preservedMutationBackedRefreshWriteVersions = new Set<number>();
  } else if (fromActiveRefresh || !snapshot.event || snapshot.event.eventUuid !== mutationBackedEventUuid) {
    clearMutationBackedPreservation();
  }

  appliedWriteVersion = writeVersion;
  if (areActiveCurtailmentSnapshotsEqual(currentSnapshot, snapshot)) {
    return currentSnapshot;
  }

  useActiveCurtailmentDataStore.setState(snapshot);
  return snapshot;
}

export function getActiveCurtailmentSnapshot(): ActiveCurtailmentSnapshot {
  const { event, events } = useActiveCurtailmentDataStore.getState();
  return { event, events };
}

export function useActiveCurtailmentEvent(): ProtoCurtailmentEvent | undefined {
  return useActiveCurtailmentDataStore((state) => state.event);
}

export function useActiveCurtailmentEvents(): ProtoCurtailmentEvent[] {
  return useActiveCurtailmentDataStore((state) => state.events);
}

export function applyActiveCurtailmentEvent(
  event?: ProtoCurtailmentEvent,
  options: ApplyActiveCurtailmentEventOptions = {},
): ActiveCurtailmentSnapshot {
  if (!event) {
    return setActiveCurtailmentSnapshot(initialSnapshot, undefined, options);
  }

  return setActiveCurtailmentSnapshot(
    {
      event,
      events: mergeActiveCurtailmentEventList(
        options.mergeActiveEvents ? getActiveCurtailmentSnapshot().events : [],
        event,
      ),
    },
    undefined,
    options,
  );
}

export function dismissActiveCurtailmentEvent(eventUuid?: string | null): ActiveCurtailmentSnapshot {
  dismissedEventUuid = eventUuid ?? getActiveCurtailmentSnapshot().event?.eventUuid ?? null;
  return setActiveCurtailmentSnapshot(initialSnapshot);
}

function shouldPreserveTerminalActiveCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return (
    event.state === CurtailmentEventState.COMPLETED || event.state === CurtailmentEventState.COMPLETED_WITH_FAILURES
  );
}

function getActiveCurtailmentSnapshotFromResponse(
  event: ProtoCurtailmentEvent | undefined,
  events: ProtoCurtailmentEvent[],
): ActiveCurtailmentResponseSnapshot {
  if (event) {
    return { snapshot: { event, events: mergeActiveCurtailmentEventList(events, event) } };
  }

  const currentSnapshot = getActiveCurtailmentSnapshot();
  if (currentSnapshot.event && shouldPreserveTerminalActiveCurtailmentEvent(currentSnapshot.event)) {
    return { snapshot: { event: currentSnapshot.event, events } };
  }

  return { snapshot: { event: undefined, events } };
}

function getSelectedActiveCurtailmentSummary(events: ProtoCurtailmentEvent[]): ProtoCurtailmentEvent | undefined {
  const currentEventUuid = getActiveCurtailmentSnapshot().event?.eventUuid;
  if (!currentEventUuid) {
    return events[0];
  }

  return events.find((event) => event.eventUuid === currentEventUuid) ?? events[0];
}

async function requestActiveCurtailmentDetail(
  eventUuid: string,
  signal: AbortSignal,
): Promise<ProtoCurtailmentEvent | undefined> {
  const response = await curtailmentClient.getCurtailmentEvent(
    createMessage(GetCurtailmentEventRequestSchema, {
      eventUuid,
      targetPageSize: activeCurtailmentDetailTargetPageSize,
    }),
    { signal },
  );
  assertNotAborted(signal);
  return response.event;
}

async function requestActiveCurtailmentResponseSnapshot(
  signal: AbortSignal,
): Promise<ActiveCurtailmentResponseSnapshot> {
  const response = await curtailmentClient.listActiveCurtailments(
    createMessage(ListActiveCurtailmentsRequestSchema, {}),
    { signal },
  );
  assertNotAborted(signal);

  const selectedEvent = getSelectedActiveCurtailmentSummary(response.events);
  if (!selectedEvent) {
    return getActiveCurtailmentSnapshotFromResponse(undefined, response.events);
  }

  const detailedEvent = await requestActiveCurtailmentDetail(selectedEvent.eventUuid, signal);
  return getActiveCurtailmentSnapshotFromResponse(detailedEvent ?? selectedEvent, response.events);
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
    promise: requestActiveCurtailmentResponseSnapshot(abortController.signal)
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
    const { snapshot } = await request.promise;
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
    commit: () =>
      setActiveCurtailmentSnapshot(snapshot, writeVersion, {
        fromActiveRefresh: true,
      }),
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
  clearMutationBackedPreservation();
  appliedWriteVersion = getNextWriteVersion();
  useActiveCurtailmentDataStore.setState(initialSnapshot, true);
}
