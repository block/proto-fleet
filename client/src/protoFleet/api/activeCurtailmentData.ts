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
import { assertNotAborted, isAbortError, isAuthOrPermissionError } from "@/protoFleet/api/requestErrors";

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
  preserveEventUuid?: string;
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
  preserveEventUuid?: string;
}

const activeCurtailmentDetailTargetPageSize = 1000;
const activeCurtailmentDetailMaxTargetPages = 25;
const mutationBackedMissingRefreshPreserveMs = 30_000;
const detailReductionSnapshotKeys = ["estimated_reduction_kw", "estimatedReductionKw"] as const;
const detailSelectedCountSnapshotKeys = ["selected_count", "selectedCount"] as const;

const initialSnapshot: ActiveCurtailmentSnapshot = { event: undefined, events: [] };

const useActiveCurtailmentDataStore = createStore<ActiveCurtailmentSnapshot>(() => initialSnapshot);

let nextWriteVersion = 0;
let appliedWriteVersion = 0;
let inFlightActiveCurtailmentRequest: InFlightActiveCurtailmentRequest | null = null;
let dismissedEventUuid: string | null = null;
let mutationBackedEventUuid: string | null = null;
let preservedMutationBackedRefreshWriteVersions = new Set<number>();
let mutationBackedPreserveUntilMs = 0;

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

function shouldPreserveSelectedActiveCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return (
    event.state === CurtailmentEventState.RESTORING ||
    event.state === CurtailmentEventState.COMPLETED ||
    event.state === CurtailmentEventState.COMPLETED_WITH_FAILURES
  );
}

function shouldSelectActiveCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return isListedActiveCurtailmentEvent(event) || shouldPreserveSelectedActiveCurtailmentEvent(event);
}

function getMutationStateRank(event: ProtoCurtailmentEvent): number {
  switch (event.state) {
    case CurtailmentEventState.PENDING:
      return 1;
    case CurtailmentEventState.ACTIVE:
      return 2;
    case CurtailmentEventState.RESTORING:
      return 3;
    case CurtailmentEventState.COMPLETED:
    case CurtailmentEventState.COMPLETED_WITH_FAILURES:
    case CurtailmentEventState.CANCELLED:
    case CurtailmentEventState.FAILED:
      return 4;
    default:
      return 0;
  }
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

function hasSnapshotNumber(event: ProtoCurtailmentEvent, keys: readonly string[]): boolean {
  return keys.some((key) => typeof event.decisionSnapshot?.[key] === "number");
}

// Live rollups now ride on every active-list row, so rollup presence no
// longer distinguishes hydrated detail from a summary row; auto-selection
// still requires snapshot numbers or target rows so the active card never
// renders a fabricated 0.0 kW estimate from a summary-only event.
function hasActiveCurtailmentDetail(event: ProtoCurtailmentEvent): boolean {
  return (
    event.targets.length > 0 ||
    hasSnapshotNumber(event, detailReductionSnapshotKeys) ||
    hasSnapshotNumber(event, detailSelectedCountSnapshotKeys)
  );
}

function getNextSelectedActiveCurtailmentEvent(
  event: ProtoCurtailmentEvent | undefined,
  events: ProtoCurtailmentEvent[],
  excludedEventUuid: string,
): ProtoCurtailmentEvent | undefined {
  if (event && event.eventUuid !== excludedEventUuid) {
    return event;
  }

  return events.find(hasActiveCurtailmentDetail);
}

function filterDismissedActiveCurtailmentEvent(
  snapshot: ActiveCurtailmentSnapshot,
  fromActiveRefresh: boolean,
): ActiveCurtailmentSnapshot {
  if (!dismissedEventUuid) {
    return snapshot;
  }

  const filteredEventUuid = dismissedEventUuid;
  const eventWasDismissed = snapshot.event?.eventUuid === filteredEventUuid;
  const eventsHadDismissedEvent = snapshot.events.some((event) => event.eventUuid === filteredEventUuid);
  const events = removeActiveCurtailmentEventFromList(snapshot.events, filteredEventUuid);

  if (fromActiveRefresh && !eventWasDismissed && !eventsHadDismissedEvent) {
    dismissedEventUuid = null;
  }

  return {
    event: getNextSelectedActiveCurtailmentEvent(snapshot.event, events, filteredEventUuid),
    events,
  };
}

function getMutationBackedEventFromSnapshot(snapshot: ActiveCurtailmentSnapshot): ProtoCurtailmentEvent | undefined {
  if (!mutationBackedEventUuid) {
    return undefined;
  }

  if (snapshot.event?.eventUuid === mutationBackedEventUuid) {
    return snapshot.event;
  }

  return snapshot.events.find((event) => event.eventUuid === mutationBackedEventUuid);
}

function shouldPreserveMutationBackedSnapshot(
  current: ActiveCurtailmentSnapshot,
  next: ActiveCurtailmentSnapshot,
  writeVersion: number,
): boolean {
  const currentMutationBackedEvent = getMutationBackedEventFromSnapshot(current);
  if (!mutationBackedEventUuid || !currentMutationBackedEvent) {
    return preservedMutationBackedRefreshWriteVersions.has(writeVersion);
  }

  if (preservedMutationBackedRefreshWriteVersions.has(writeVersion)) {
    return true;
  }

  const nextMutationBackedEvent = getMutationBackedEventFromSnapshot(next);
  if (!nextMutationBackedEvent) {
    return Date.now() < mutationBackedPreserveUntilMs;
  }

  if (equals(CurtailmentEventSchema, currentMutationBackedEvent, nextMutationBackedEvent)) {
    return false;
  }

  return getMutationStateRank(nextMutationBackedEvent) < getMutationStateRank(currentMutationBackedEvent);
}

function clearMutationBackedPreservation(): void {
  mutationBackedEventUuid = null;
  preservedMutationBackedRefreshWriteVersions = new Set<number>();
  mutationBackedPreserveUntilMs = 0;
}

export function clearMutationBackedActiveCurtailmentEvent(eventUuid: string): void {
  if (mutationBackedEventUuid === eventUuid) {
    clearMutationBackedPreservation();
  }
}

function setActiveCurtailmentSnapshot(
  snapshot: ActiveCurtailmentSnapshot,
  writeVersion = getNextWriteVersion(),
  {
    fromActiveRefresh = false,
    preserveAgainstStaleRefresh = false,
    preserveEventUuid,
  }: SetActiveCurtailmentSnapshotOptions = {},
): ActiveCurtailmentSnapshot {
  if (writeVersion < appliedWriteVersion) {
    return getActiveCurtailmentSnapshot();
  }

  snapshot = filterDismissedActiveCurtailmentEvent(snapshot, fromActiveRefresh);

  const currentSnapshot = getActiveCurtailmentSnapshot();
  if (fromActiveRefresh && shouldPreserveMutationBackedSnapshot(currentSnapshot, snapshot, writeVersion)) {
    preservedMutationBackedRefreshWriteVersions.add(writeVersion);
    return currentSnapshot;
  }

  if (preserveAgainstStaleRefresh && preserveEventUuid) {
    mutationBackedEventUuid = preserveEventUuid;
    preservedMutationBackedRefreshWriteVersions = new Set<number>();
    mutationBackedPreserveUntilMs = Date.now() + mutationBackedMissingRefreshPreserveMs;
  } else if (fromActiveRefresh || (mutationBackedEventUuid && !getMutationBackedEventFromSnapshot(snapshot))) {
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

  const currentSnapshot = getActiveCurtailmentSnapshot();
  const events = mergeActiveCurtailmentEventList(options.mergeActiveEvents ? currentSnapshot.events : [], event);
  const selectedEvent = shouldSelectActiveCurtailmentEvent(event)
    ? event
    : getNextSelectedActiveCurtailmentEvent(currentSnapshot.event, events, event.eventUuid);

  return setActiveCurtailmentSnapshot(
    {
      event: selectedEvent,
      events,
    },
    undefined,
    {
      ...options,
      preserveEventUuid: options.preserveAgainstStaleRefresh ? event.eventUuid : options.preserveEventUuid,
    },
  );
}

export function preserveActiveCurtailmentEvents(eventsToPreserve: ProtoCurtailmentEvent[]): ActiveCurtailmentSnapshot {
  const currentSnapshot = getActiveCurtailmentSnapshot();
  const events = eventsToPreserve.reduce<ProtoCurtailmentEvent[]>((nextEvents, eventToPreserve) => {
    if (!isListedActiveCurtailmentEvent(eventToPreserve)) {
      return nextEvents;
    }

    const eventIndex = nextEvents.findIndex((event) => event.eventUuid === eventToPreserve.eventUuid);
    if (eventIndex === -1) {
      return [...nextEvents, eventToPreserve];
    }

    return nextEvents.map((event, index) => (index === eventIndex ? eventToPreserve : event));
  }, currentSnapshot.events);

  return setActiveCurtailmentSnapshot({
    event: currentSnapshot.event,
    events,
  });
}

export function dismissActiveCurtailmentEvent(eventUuid?: string | null): ActiveCurtailmentSnapshot {
  const currentSnapshot = getActiveCurtailmentSnapshot();
  const dismissedUuid = eventUuid ?? currentSnapshot.event?.eventUuid ?? null;
  if (!dismissedUuid) {
    return currentSnapshot;
  }

  dismissedEventUuid = dismissedUuid;
  const events = removeActiveCurtailmentEventFromList(currentSnapshot.events, dismissedUuid);
  return setActiveCurtailmentSnapshot({
    event: getNextSelectedActiveCurtailmentEvent(currentSnapshot.event, events, dismissedUuid),
    events,
  });
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

function getSelectedActiveCurtailmentWithCurrentDetail(
  selectedEvent: ProtoCurtailmentEvent,
): ProtoCurtailmentEvent | undefined {
  const currentEvent = getActiveCurtailmentSnapshot().event;
  if (currentEvent?.eventUuid !== selectedEvent.eventUuid) {
    return undefined;
  }

  // The active-list row is fresher than the current detail, so its live
  // target rollup wins; current detail only backfills fields the list row
  // never provides (decision snapshot, targets) or lacks (older servers'
  // rollup-less list rows).
  return createMessage(CurtailmentEventSchema, {
    ...selectedEvent,
    decisionSnapshot: currentEvent.decisionSnapshot,
    targetRollup: selectedEvent.targetRollup ?? currentEvent.targetRollup,
    targets: currentEvent.targets,
  });
}

function getSelectedActiveCurtailmentEventToPreserve(
  events: ProtoCurtailmentEvent[],
): ProtoCurtailmentEvent | undefined {
  const currentEvent = getActiveCurtailmentSnapshot().event;
  if (!currentEvent || events.some((event) => event.eventUuid === currentEvent.eventUuid)) {
    return undefined;
  }

  return shouldPreserveTerminalActiveCurtailmentEvent(currentEvent) ? currentEvent : undefined;
}

async function requestActiveCurtailmentDetail(
  eventUuid: string,
  { hydrateAllTargetPages = false, signal }: RefreshActiveCurtailmentOptions & { hydrateAllTargetPages?: boolean } = {},
): Promise<ProtoCurtailmentEvent | undefined> {
  let pageToken = "";
  let detailedEvent: ProtoCurtailmentEvent | undefined;
  const targets: ProtoCurtailmentEvent["targets"] = [];
  const seenPageTokens = new Set<string>();
  let pageCount = 0;

  while (true) {
    seenPageTokens.add(pageToken);
    pageCount += 1;
    const response = await curtailmentClient.getCurtailmentEvent(
      createMessage(GetCurtailmentEventRequestSchema, {
        eventUuid,
        targetPageSize: activeCurtailmentDetailTargetPageSize,
        targetPageToken: pageToken,
      }),
      signal ? { signal } : undefined,
    );
    assertNotAborted(signal);

    if (response.event) {
      detailedEvent = response.event;
      targets.push(...response.event.targets);
    }

    if (!response.nextTargetPageToken || seenPageTokens.has(response.nextTargetPageToken)) {
      break;
    }

    if (!hydrateAllTargetPages) {
      // Polling only needs event-level detail. Drop the partial target page so
      // target-derived metrics do not reuse stale samples from older detail.
      return detailedEvent ? createMessage(CurtailmentEventSchema, { ...detailedEvent, targets: [] }) : undefined;
    }

    if (pageCount >= activeCurtailmentDetailMaxTargetPages) {
      return detailedEvent ? createMessage(CurtailmentEventSchema, { ...detailedEvent, targets: [] }) : undefined;
    }

    pageToken = response.nextTargetPageToken;
  }

  return detailedEvent ? createMessage(CurtailmentEventSchema, { ...detailedEvent, targets }) : undefined;
}

export async function selectActiveCurtailmentEvent(
  eventUuid: string,
  { signal }: RefreshActiveCurtailmentOptions = {},
): Promise<ActiveCurtailmentSnapshot> {
  assertNotAborted(signal);
  const detailedEvent = await requestActiveCurtailmentDetail(eventUuid, { hydrateAllTargetPages: true, signal });
  assertNotAborted(signal);

  const latestSnapshot = getActiveCurtailmentSnapshot();
  const summaryEvent = latestSnapshot.events.find((event) => event.eventUuid === eventUuid);
  if (!summaryEvent) {
    return latestSnapshot;
  }

  return applyActiveCurtailmentEvent(detailedEvent ?? summaryEvent, { mergeActiveEvents: true });
}

async function requestActiveCurtailmentResponseSnapshot(
  signal: AbortSignal,
): Promise<ActiveCurtailmentResponseSnapshot> {
  const response = await curtailmentClient.listActiveCurtailments(
    createMessage(ListActiveCurtailmentsRequestSchema, {}),
    { signal },
  );
  assertNotAborted(signal);

  const preservedSelectedEvent = getSelectedActiveCurtailmentEventToPreserve(response.events);
  if (preservedSelectedEvent) {
    return getActiveCurtailmentSnapshotFromResponse(preservedSelectedEvent, response.events);
  }

  const selectedEvent = getSelectedActiveCurtailmentSummary(response.events);
  if (!selectedEvent) {
    return getActiveCurtailmentSnapshotFromResponse(undefined, response.events);
  }

  let detailedSelectedEvent: ProtoCurtailmentEvent | undefined;
  try {
    detailedSelectedEvent = await requestActiveCurtailmentDetail(selectedEvent.eventUuid, { signal });
  } catch (error) {
    if (isAbortError(error, signal)) {
      throw error;
    }
    if (isAuthOrPermissionError(error)) {
      throw error;
    }
  }

  return getActiveCurtailmentSnapshotFromResponse(
    detailedSelectedEvent ?? getSelectedActiveCurtailmentWithCurrentDetail(selectedEvent),
    response.events,
  );
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
