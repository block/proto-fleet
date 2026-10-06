import { type KeyboardEvent, type MouseEvent, type ReactElement, useMemo, useState } from "react";
import clsx from "clsx";

import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import {
  type ActiveCurtailmentDisplayState,
  activeCurtailmentDisplayStateConfigs,
  type CurtailmentEventState,
  curtailmentEventStateConfigs,
  curtailmentEventStates,
  formatCurtailmentAppliesToSummary,
  formatCurtailmentFacilityFanCount,
  formatCurtailmentTargetVsActual as formatTargetVsActual,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { type ButtonVariant, sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import FilterChip from "@/shared/components/List/Filters/FilterChip";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";

export type CurtailmentPriority = "normal" | "high" | "emergency";

export interface CurtailmentHistoryEvent {
  id: string;
  reason: string;
  state: CurtailmentEventState;
  displayState?: ActiveCurtailmentDisplayState;
  injectedActive?: boolean;
  priority: CurtailmentPriority;
  scopeLabel: string;
  selectedMiners: number;
  facilityFanDeviceCount?: number;
  estimatedReductionKw: number;
  targetMetricsAvailable: boolean;
  // Distinct from targetMetricsAvailable: live rollups prove counts without
  // proving a kW estimate (active-list rows scrub the decision snapshot).
  // Optional so fixture-driven surfaces default to the count signal.
  estimatedReductionAvailable?: boolean;
  targetKw?: number;
  sourceLabel: string;
  startedAt?: string;
  endedAt?: string;
  scheduledAt?: string;
  createdAt?: string;
}

interface CurtailmentHistoryProps {
  events: CurtailmentHistoryEvent[];
  activeEventId?: string;
  activeEventIds?: string[];
  pageSize?: number;
  currentPage?: number;
  hasNextPage?: boolean;
  hasPreviousPage?: boolean;
  selectedStatusFilter?: CurtailmentEventState;
  selectedStatusFilters?: CurtailmentEventState[];
  className?: string;
  onViewEvent?: (event: CurtailmentHistoryEvent) => void;
  onPageChange?: (page: number) => void;
  onStatusFilterChange?: (filter?: CurtailmentEventState) => void;
  onStatusFiltersChange?: (filters: CurtailmentEventState[]) => void;
  /**
   * Called from the detail modal for the active non-terminal event. The parent
   * owns opening the edit flow with the selected event's values.
   */
  onManageActiveEvent?: (event: CurtailmentHistoryEvent) => void;
  /**
   * Called from the detail modal for active restoring events that cannot be
   * edited or stopped directly. The parent owns selecting/hydrating the event.
   */
  onSelectActiveEvent?: (event: CurtailmentHistoryEvent) => void;
  /**
   * Called after the operator confirms stopping the active event. The parent
   * owns persistence. Return a promise only after an actual stop request
   * starts; a rejected promise re-enables retry controls.
   */
  onStopActiveEvent?: (event: CurtailmentHistoryEvent) => void | Promise<unknown>;
  onStopActiveEventRequested?: (event: CurtailmentHistoryEvent) => void;
}

interface DotProps {
  className: string;
}

interface DetailRowProps {
  label: string;
  value: string;
  secondary?: string;
}

interface CurtailmentSummaryModalProps {
  event: CurtailmentHistoryEvent;
  open: boolean;
  onDismiss: () => void;
  onManage?: () => void;
  onSelectActive?: () => void;
  onStop?: () => void;
  stopDisabled?: boolean;
}

interface CurtailmentHistoryRowProps {
  event: CurtailmentHistoryEvent;
  activeEventIds: Set<string>;
  onOpenSummary: (event: CurtailmentHistoryEvent) => void;
  onRequestStop?: (event: CurtailmentHistoryEvent) => void;
  stopDisabled?: boolean;
}

interface CurtailmentSummaryModalButton {
  text: string;
  variant: ButtonVariant;
  onClick: () => void;
  dismissModalOnClick: false;
  disabled?: boolean;
}

const defaultPageSize = 50;
const stoppableEventStates = new Set<CurtailmentEventState>(["pending", "active"]);
const manageableEventStates = new Set<CurtailmentEventState>(["pending", "active"]);
const selectableEventStates = new Set<CurtailmentEventState>(["restoring"]);
const rowInteractiveElementSelector =
  'button, a, input, select, textarea, [role="button"], [role="link"], [data-interactive]';
const unavailableTargetMetricsLabel = "Target details unavailable";

const priorityLabels: Record<CurtailmentPriority, string> = {
  normal: "Normal",
  high: "High",
  emergency: "Emergency",
};

const statusFilterOptions = curtailmentEventStates.map((state) => ({
  id: state,
  label: curtailmentEventStateConfigs[state].label,
}));
const statusFilterOptionIds = new Set<string>(curtailmentEventStates);

const historyColumns = ["event", "scope", "target", "state"] as const;

type HistoryColumn = (typeof historyColumns)[number];

const historyColumnLabels: Record<HistoryColumn, string> = {
  event: "Event",
  scope: "Applies to",
  target: "Target vs actual",
  state: "Status",
};

const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });
const dateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
});

function Dot({ className }: DotProps): ReactElement {
  return <span className={clsx("inline-block h-2 w-2 shrink-0 rounded-full", className)} />;
}

function DetailRow({ label, value, secondary }: DetailRowProps): ReactElement {
  return (
    <div className="grid grid-cols-[minmax(120px,0.42fr)_minmax(0,1fr)] gap-4 border-b border-border-5 py-3 last:border-0">
      <div className="text-300 text-text-primary-50">{label}</div>
      <div className="min-w-0 text-right">
        <div className="truncate text-300 text-text-primary" title={value}>
          {value}
        </div>
        {secondary ? (
          <div className="truncate text-200 text-text-primary-50" title={secondary}>
            {secondary}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function formatHistoryAppliesToSummary(event: CurtailmentHistoryEvent): string {
  const facilityFanDeviceCount = event.facilityFanDeviceCount ?? 0;

  if (event.targetMetricsAvailable) {
    return formatCurtailmentAppliesToSummary(event.selectedMiners, facilityFanDeviceCount);
  }

  if (facilityFanDeviceCount > 0) {
    return `${unavailableTargetMetricsLabel}, ${formatCurtailmentFacilityFanCount(facilityFanDeviceCount)}`;
  }

  return unavailableTargetMetricsLabel;
}

function formatHistoryTargetVsActual(event: CurtailmentHistoryEvent): string {
  if (!event.targetMetricsAvailable) {
    return unavailableTargetMetricsLabel;
  }

  // Summary-only active rows carry live counts but no kW estimate; showing
  // "target / 0.0 kW" would fabricate a zero estimate.
  if (event.estimatedReductionAvailable === false) {
    return unavailableTargetMetricsLabel;
  }

  return formatTargetVsActual(event);
}

function getDateTime(value?: string): Date | undefined {
  if (!value) {
    return undefined;
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

function formatDateTime(value?: string): string | undefined {
  const date = getDateTime(value);
  return date ? dateTimeFormatter.format(date) : undefined;
}

function getHistoryEventStateConfig(event: CurtailmentHistoryEvent): {
  label: string;
  dotClassName: string;
} {
  return event.displayState
    ? activeCurtailmentDisplayStateConfigs[event.displayState]
    : curtailmentEventStateConfigs[event.state];
}

function getHistoryStatusDetail(event: CurtailmentHistoryEvent): string {
  const endedAt = formatDateTime(event.endedAt);
  if (endedAt) {
    return `Ended ${endedAt}`;
  }

  const startedAt = formatDateTime(event.startedAt);
  if (startedAt) {
    return `Started ${startedAt}`;
  }

  const scheduledAt = formatDateTime(event.scheduledAt);
  if (scheduledAt) {
    return `Scheduled ${scheduledAt}`;
  }

  const createdAt = formatDateTime(event.createdAt);
  if (createdAt) {
    return `Created ${createdAt}`;
  }

  return (event.displayState ?? event.state) === "pending" ? "Waiting to start" : "Time unavailable";
}

function getSortTime(event: CurtailmentHistoryEvent): number {
  return (
    getDateTime(event.startedAt)?.getTime() ??
    getDateTime(event.scheduledAt)?.getTime() ??
    getDateTime(event.createdAt)?.getTime() ??
    0
  );
}

function compareStartedAtDesc(left: CurtailmentHistoryEvent, right: CurtailmentHistoryEvent): number {
  const dateComparison = getSortTime(right) - getSortTime(left);
  return dateComparison || collator.compare(left.id, right.id);
}

function sortHistoryEvents(events: CurtailmentHistoryEvent[]): CurtailmentHistoryEvent[] {
  return [...events].sort(compareStartedAtDesc);
}

function isActiveStoppableEvent(event: CurtailmentHistoryEvent, activeEventIds: Set<string>): boolean {
  return activeEventIds.has(event.id) && stoppableEventStates.has(event.state);
}

function isActiveManageableEvent(event: CurtailmentHistoryEvent, activeEventIds: Set<string>): boolean {
  return activeEventIds.has(event.id) && manageableEventStates.has(event.state);
}

function isActiveSelectableEvent(event: CurtailmentHistoryEvent, activeEventIds: Set<string>): boolean {
  return activeEventIds.has(event.id) && selectableEventStates.has(event.state);
}

function isPromiseLike(value: unknown): value is PromiseLike<unknown> {
  if (value === null || (typeof value !== "object" && typeof value !== "function")) {
    return false;
  }

  return typeof (value as { then?: unknown }).then === "function";
}

function getNormalizedPageSize(pageSize: number): number {
  return Number.isFinite(pageSize) && pageSize >= 1 ? Math.floor(pageSize) : defaultPageSize;
}

function getNormalizedPageIndex(page: number): number {
  return Number.isFinite(page) && page > 0 ? Math.floor(page) : 0;
}

function isCurtailmentEventState(filter: string): filter is CurtailmentEventState {
  return statusFilterOptionIds.has(filter);
}

function normalizeStatusFilters(filters: string[]): CurtailmentEventState[] {
  return filters.filter(isCurtailmentEventState);
}

function getActiveEventIdSet(activeEventId?: string, activeEventIds: readonly string[] = []): Set<string> {
  return new Set([activeEventId, ...activeEventIds].filter((eventId): eventId is string => Boolean(eventId)));
}

function shouldIgnoreRowActivation(eventTarget: EventTarget | null, currentTarget: HTMLTableRowElement): boolean {
  if (!(eventTarget instanceof Element) || !currentTarget.contains(eventTarget)) {
    return true;
  }

  const interactiveElement = eventTarget.closest(rowInteractiveElementSelector);
  return Boolean(
    (interactiveElement && interactiveElement !== currentTarget) || eventTarget.closest("[data-no-row-click]"),
  );
}

function CurtailmentSummaryModal({
  event,
  open,
  onDismiss,
  onManage,
  onSelectActive,
  onStop,
  stopDisabled,
}: CurtailmentSummaryModalProps): ReactElement {
  const createdAt = formatDateTime(event.createdAt);
  const endedAt = formatDateTime(event.endedAt);
  // Ended events can lack startedAt from the backend; use createdAt so completed history never reads as unstarted.
  const startedAt = formatDateTime(event.startedAt) ?? (endedAt ? createdAt : undefined);
  const scheduledAt = formatDateTime(event.scheduledAt);
  const eventStateConfig = getHistoryEventStateConfig(event);
  const buttons: CurtailmentSummaryModalButton[] = [];

  if (onStop) {
    buttons.push({
      text: "Stop curtailment",
      variant: variants.secondaryDanger,
      onClick: onStop,
      dismissModalOnClick: false,
      disabled: stopDisabled,
    });
  }

  if (onManage) {
    buttons.push({
      text: "Manage",
      variant: variants.primary,
      onClick: onManage,
      dismissModalOnClick: false,
    });
  }

  if (onSelectActive) {
    buttons.push({
      text: "View active event",
      variant: variants.primary,
      onClick: onSelectActive,
      dismissModalOnClick: false,
    });
  }

  return (
    <Modal
      open={open}
      size={modalSizes.standard}
      title="Curtailment detail"
      onDismiss={onDismiss}
      bodyClassName="text-text-primary"
      buttons={buttons}
    >
      <section>
        <div className="mb-2 text-emphasis-300 text-text-primary">Details</div>
        <div className="rounded-xl border border-border-5 px-4">
          <DetailRow label="Event" value={event.reason} />
          <DetailRow label="ID" value={event.id} />
          <DetailRow label="Applies to" value={event.scopeLabel} secondary={formatHistoryAppliesToSummary(event)} />
          <DetailRow label="Power target vs actual" value={formatHistoryTargetVsActual(event)} />
          <DetailRow label="Status" value={eventStateConfig.label} />
          <DetailRow label="Started" value={startedAt ?? "Not started yet"} />
          {scheduledAt ? <DetailRow label="Scheduled" value={scheduledAt} /> : null}
          {endedAt ? <DetailRow label="Ended" value={endedAt} /> : null}
          <DetailRow label="Type" value={priorityLabels[event.priority]} />
          <DetailRow label="Source" value={event.sourceLabel} />
        </div>
      </section>
    </Modal>
  );
}

function CurtailmentHistoryRow({
  event,
  activeEventIds,
  onOpenSummary,
  onRequestStop,
  stopDisabled,
}: CurtailmentHistoryRowProps): ReactElement {
  const canStop = Boolean(onRequestStop) && isActiveStoppableEvent(event, activeEventIds);
  const eventStateConfig = getHistoryEventStateConfig(event);

  const handleRowClick = (clickEvent: MouseEvent<HTMLTableRowElement>) => {
    if (shouldIgnoreRowActivation(clickEvent.target, clickEvent.currentTarget)) {
      return;
    }

    onOpenSummary(event);
  };

  const handleRowKeyDown = (keyboardEvent: KeyboardEvent<HTMLTableRowElement>) => {
    const isEnterKey = keyboardEvent.key === "Enter";
    const isSpaceKey = keyboardEvent.key === " ";

    if (!isEnterKey && !isSpaceKey) {
      return;
    }

    if (shouldIgnoreRowActivation(keyboardEvent.target, keyboardEvent.currentTarget)) {
      return;
    }

    keyboardEvent.preventDefault();
    onOpenSummary(event);
  };

  const handleStopClick = (clickEvent: MouseEvent<HTMLButtonElement>) => {
    clickEvent.stopPropagation();
    onRequestStop?.(event);
  };

  return (
    <tr
      className="cursor-pointer border-b border-border-5 transition-colors last:border-0 hover:bg-core-primary-5"
      tabIndex={0}
      aria-label={`View curtailment ${event.reason}`}
      onClick={handleRowClick}
      onKeyDown={handleRowKeyDown}
      data-testid={`curtailment-history-row-${event.id}`}
    >
      <td className="py-4 pr-6 align-top">
        <div className="truncate text-emphasis-300 text-text-primary" title={event.reason}>
          {event.reason}
        </div>
        <div className="truncate font-mono text-200 text-text-primary-50" title={event.id}>
          {event.id}
        </div>
      </td>
      <td className="py-4 pr-6 align-top">
        <div className="truncate text-text-primary" title={event.scopeLabel}>
          {event.scopeLabel}
        </div>
        <div className="text-200 text-text-primary-50">{formatHistoryAppliesToSummary(event)}</div>
      </td>
      <td className="py-4 pr-6 align-top text-text-primary">{formatHistoryTargetVsActual(event)}</td>
      <td className="py-4 pr-6 align-top">
        <div className="flex items-center gap-2 text-emphasis-300 text-text-primary">
          <Dot className={eventStateConfig.dotClassName} />
          {eventStateConfig.label}
        </div>
        <div className="text-200 text-text-primary-50">{getHistoryStatusDetail(event)}</div>
      </td>
      <td className="py-4 align-top" data-no-row-click={canStop ? "true" : undefined}>
        <div className="flex justify-end gap-2">
          {canStop ? (
            <Button
              variant={variants.danger}
              size={sizes.compact}
              text="Stop"
              ariaLabel={`Stop ${event.reason}`}
              onClick={handleStopClick}
              disabled={stopDisabled}
            />
          ) : null}
        </div>
      </td>
    </tr>
  );
}

function CurtailmentHistory({
  events,
  activeEventId,
  activeEventIds: activeEventIdList = [],
  pageSize = defaultPageSize,
  currentPage: controlledCurrentPage = 0,
  hasNextPage: controlledHasNextPage = false,
  hasPreviousPage: controlledHasPreviousPage,
  selectedStatusFilter,
  selectedStatusFilters: controlledSelectedStatusFilters,
  className,
  onViewEvent,
  onPageChange,
  onStatusFilterChange,
  onStatusFiltersChange,
  onManageActiveEvent,
  onSelectActiveEvent,
  onStopActiveEvent,
  onStopActiveEventRequested,
}: CurtailmentHistoryProps): ReactElement {
  const [localSelectedStatusFilters, setLocalSelectedStatusFilters] = useState<CurtailmentEventState[]>([]);
  const [currentPage, setCurrentPage] = useState(0);
  const [selectedDetailEventId, setSelectedDetailEventId] = useState<string>();
  const [selectedStopEventId, setSelectedStopEventId] = useState<string>();
  const [pendingStopEventIds, setPendingStopEventIds] = useState<Set<string>>(() => new Set());
  const activeEventIds = useMemo(
    () => getActiveEventIdSet(activeEventId, activeEventIdList),
    [activeEventId, activeEventIdList],
  );
  const normalizedPageSize = getNormalizedPageSize(pageSize);
  const selectedDetailEvent = useMemo(
    () => events.find((event) => event.id === selectedDetailEventId),
    [events, selectedDetailEventId],
  );
  const selectedStopEvent = useMemo(
    () => events.find((event) => event.id === selectedStopEventId),
    [events, selectedStopEventId],
  );
  const selectedStopEventIsStoppable = Boolean(
    selectedStopEvent && isActiveStoppableEvent(selectedStopEvent, activeEventIds),
  );
  const usesControlledPagination = Boolean(onPageChange);
  const usesServerStatusFilter = Boolean(onStatusFiltersChange || onStatusFilterChange);
  const selectedStatusFilters = useMemo(
    () =>
      usesServerStatusFilter
        ? normalizeStatusFilters(
            controlledSelectedStatusFilters ?? (selectedStatusFilter ? [selectedStatusFilter] : []),
          )
        : localSelectedStatusFilters,
    [controlledSelectedStatusFilters, localSelectedStatusFilters, selectedStatusFilter, usesServerStatusFilter],
  );
  const hasActiveFilters = selectedStatusFilters.length > 0;

  const filteredEvents = useMemo(() => {
    if (usesServerStatusFilter || selectedStatusFilters.length === 0) {
      return events;
    }

    const filterSet = new Set(selectedStatusFilters);
    return events.filter((event) => filterSet.has(event.state));
  }, [events, selectedStatusFilters, usesServerStatusFilter]);

  const sortedEvents = useMemo(() => sortHistoryEvents(filteredEvents), [filteredEvents]);
  const totalEvents = sortedEvents.length;
  const pageCount = usesControlledPagination ? 1 : Math.max(Math.ceil(totalEvents / normalizedPageSize), 1);
  const effectiveCurrentPage = usesControlledPagination
    ? getNormalizedPageIndex(controlledCurrentPage)
    : Math.min(currentPage, pageCount - 1);
  const firstVisibleEventIndex = usesControlledPagination ? 0 : effectiveCurrentPage * normalizedPageSize;
  const visibleEvents = usesControlledPagination
    ? sortedEvents
    : sortedEvents.slice(firstVisibleEventIndex, firstVisibleEventIndex + normalizedPageSize);
  const firstItemIndex = firstVisibleEventIndex + 1;
  const lastItemIndex = firstItemIndex + visibleEvents.length - 1;
  const serverFirstItemIndex = effectiveCurrentPage * normalizedPageSize + 1;
  const serverLastItemIndex = serverFirstItemIndex + visibleEvents.length - 1;
  const shouldRenderPagination = usesControlledPagination
    ? events.length > 0 || effectiveCurrentPage > 0 || controlledHasNextPage
    : totalEvents > 0;
  const hasPreviousPage = usesControlledPagination
    ? (controlledHasPreviousPage ?? effectiveCurrentPage > 0)
    : effectiveCurrentPage > 0;
  const hasNextPage = usesControlledPagination ? controlledHasNextPage : lastItemIndex < totalEvents;

  const handleStatusFilterChange = (filters: string[]) => {
    const nextFilters = normalizeStatusFilters(filters);
    if (onStatusFiltersChange) {
      onStatusFiltersChange(nextFilters);
      return;
    }

    if (onStatusFilterChange) {
      onStatusFilterChange?.(nextFilters[nextFilters.length - 1]);
      return;
    }

    setLocalSelectedStatusFilters(nextFilters);
    if (usesControlledPagination) {
      onPageChange?.(0);
    } else {
      setCurrentPage(0);
    }
  };

  const handleClearStatusFilters = () => handleStatusFilterChange([]);

  const handlePageChange = (pageDelta: number) => {
    if (usesControlledPagination) {
      onPageChange?.(Math.max(effectiveCurrentPage + pageDelta, 0));
      return;
    }

    setCurrentPage((previousPage) => Math.min(Math.max(previousPage + pageDelta, 0), pageCount - 1));
  };

  const handlePreviousPage = () => handlePageChange(-1);

  const handleNextPage = () => handlePageChange(1);

  const handleOpenSummary = (event: CurtailmentHistoryEvent) => {
    setSelectedDetailEventId(event.id);
    onViewEvent?.(event);
  };

  const handleDismissSummary = () => setSelectedDetailEventId(undefined);

  const handleOpenStopConfirmation = (event: CurtailmentHistoryEvent) => {
    if (!onStopActiveEvent || !isActiveStoppableEvent(event, activeEventIds) || pendingStopEventIds.has(event.id)) {
      return;
    }

    onStopActiveEventRequested?.(event);
    setSelectedStopEventId(event.id);
  };

  const handleDismissStopConfirmation = () => setSelectedStopEventId(undefined);

  const handleConfirmStop = () => {
    if (!selectedStopEvent || !onStopActiveEvent || !isActiveStoppableEvent(selectedStopEvent, activeEventIds)) {
      return;
    }

    const event = selectedStopEvent;
    if (pendingStopEventIds.has(event.id)) {
      setSelectedStopEventId(undefined);
      return;
    }

    setSelectedStopEventId(undefined);
    setPendingStopEventIds((currentEventIds) => new Set(currentEventIds).add(event.id));

    let stopRequest: void | PromiseLike<unknown>;
    try {
      stopRequest = onStopActiveEvent(event);
    } catch {
      setPendingStopEventIds((currentEventIds) => {
        const nextEventIds = new Set(currentEventIds);
        nextEventIds.delete(event.id);
        return nextEventIds;
      });
      return;
    }

    if (!isPromiseLike(stopRequest)) {
      return;
    }

    void Promise.resolve(stopRequest).catch(() => {
      setPendingStopEventIds((currentEventIds) => {
        const nextEventIds = new Set(currentEventIds);
        nextEventIds.delete(event.id);
        return nextEventIds;
      });
    });
  };

  const handleManageSelectedDetailEvent =
    selectedDetailEvent &&
    onManageActiveEvent &&
    isActiveManageableEvent(selectedDetailEvent, activeEventIds) &&
    !pendingStopEventIds.has(selectedDetailEvent.id)
      ? () => {
          setSelectedDetailEventId(undefined);
          onManageActiveEvent(selectedDetailEvent);
        }
      : undefined;

  const handleSelectActiveDetailEvent =
    selectedDetailEvent && onSelectActiveEvent && isActiveSelectableEvent(selectedDetailEvent, activeEventIds)
      ? () => {
          setSelectedDetailEventId(undefined);
          onSelectActiveEvent(selectedDetailEvent);
        }
      : undefined;

  const handleStopSelectedDetailEvent =
    selectedDetailEvent && onStopActiveEvent && isActiveStoppableEvent(selectedDetailEvent, activeEventIds)
      ? () => handleOpenStopConfirmation(selectedDetailEvent)
      : undefined;

  return (
    <section className={clsx("grid gap-4", className)}>
      <Header title="Curtailment history" titleSize="text-heading-200" />

      <div className="flex flex-wrap items-center gap-3">
        <DropdownFilter
          title="Status"
          pluralTitle="statuses"
          options={statusFilterOptions}
          selectedOptions={selectedStatusFilters}
          showSelectAll={false}
          onSelect={handleStatusFilterChange}
        />
        {hasActiveFilters ? (
          <FilterChip
            filterValue="status"
            title="Status"
            pluralTitle="statuses"
            options={statusFilterOptions}
            selectedIds={selectedStatusFilters}
            onChange={handleStatusFilterChange}
            onClear={handleClearStatusFilters}
          />
        ) : null}
      </div>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[760px] table-fixed text-left text-300">
          <thead>
            <tr className="border-b border-border-5 text-text-primary-50">
              {historyColumns.map((column) => (
                <th key={column} className="py-3 pr-6 font-normal">
                  <span className="text-emphasis-300 text-text-primary-50">{historyColumnLabels[column]}</span>
                </th>
              ))}
              <th className="w-24 py-3 font-normal" aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {visibleEvents.map((event) => (
              <CurtailmentHistoryRow
                key={event.id}
                event={event}
                activeEventIds={activeEventIds}
                onOpenSummary={handleOpenSummary}
                onRequestStop={onStopActiveEvent ? handleOpenStopConfirmation : undefined}
                stopDisabled={pendingStopEventIds.has(event.id)}
              />
            ))}
            {visibleEvents.length === 0 ? (
              <tr>
                <td colSpan={historyColumns.length + 1}>
                  <NoFilterResultsEmptyState
                    hasActiveFilters={hasActiveFilters}
                    onClearFilters={handleClearStatusFilters}
                  />
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      {shouldRenderPagination ? (
        <div
          className="sticky left-0 flex flex-col items-center gap-4 pt-6 pb-6"
          data-testid="curtailment-history-pagination"
        >
          <span className="text-300 text-text-primary">
            {visibleEvents.length === 0
              ? "No curtailment events on this page"
              : usesControlledPagination
                ? `Showing ${serverFirstItemIndex}–${serverLastItemIndex} curtailment events`
                : `Showing ${firstItemIndex}–${lastItemIndex} of ${totalEvents} curtailment events`}
          </span>
          <div className="flex gap-3">
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              ariaLabel="Previous page"
              prefixIcon={<ChevronDown className="rotate-90" />}
              onClick={handlePreviousPage}
              disabled={!hasPreviousPage}
            />
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              ariaLabel="Next page"
              prefixIcon={<ChevronDown className="rotate-270" />}
              onClick={handleNextPage}
              disabled={!hasNextPage}
            />
          </div>
        </div>
      ) : null}

      {selectedDetailEvent ? (
        <CurtailmentSummaryModal
          key={selectedDetailEvent.id}
          event={selectedDetailEvent}
          open
          onDismiss={handleDismissSummary}
          onManage={handleManageSelectedDetailEvent}
          onSelectActive={handleSelectActiveDetailEvent}
          onStop={handleStopSelectedDetailEvent}
          stopDisabled={pendingStopEventIds.has(selectedDetailEvent.id)}
        />
      ) : null}

      {selectedStopEvent && selectedStopEventIsStoppable ? (
        <CurtailmentStopConfirmationDialog
          open
          action="stopCurtailment"
          onCancel={handleDismissStopConfirmation}
          onConfirm={handleConfirmStop}
        />
      ) : null}
    </section>
  );
}

export default CurtailmentHistory;
