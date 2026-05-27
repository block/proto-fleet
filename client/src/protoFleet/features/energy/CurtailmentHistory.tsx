import { type KeyboardEvent, type MouseEvent, type ReactElement, useMemo, useState } from "react";
import clsx from "clsx";

import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import {
  type CurtailmentEventState,
  curtailmentEventStateConfigs,
  curtailmentEventStates,
  formatCurtailmentMinerCount as formatMinerCount,
  formatCurtailmentTargetVsActual as formatTargetVsActual,
  getCurtailmentTargetKw as getTargetKw,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
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
  priority: CurtailmentPriority;
  scopeLabel: string;
  selectedMiners: number;
  estimatedReductionKw: number;
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
  pageSize?: number;
  className?: string;
  onViewEvent?: (event: CurtailmentHistoryEvent) => void;
  /**
   * Called from the detail modal for the active non-terminal event. The parent
   * owns opening the edit flow with the selected event's values.
   */
  onManageActiveEvent?: (event: CurtailmentHistoryEvent) => void;
  /**
   * Called after the operator confirms stopping the active event. The parent
   * owns persistence. Return a promise only after an actual stop request
   * starts; a rejected promise re-enables retry controls.
   */
  onStopActiveEvent?: (event: CurtailmentHistoryEvent) => void | Promise<unknown>;
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
  onStop?: () => void;
  stopDisabled?: boolean;
}

interface CurtailmentHistoryRowProps {
  event: CurtailmentHistoryEvent;
  activeEventId?: string;
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
const manageableEventStates = new Set<CurtailmentEventState>(["pending", "active", "restoring"]);
const rowInteractiveElementSelector =
  'button, a, input, select, textarea, [role="button"], [role="link"], [data-interactive]';

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
type HistorySort = { field: HistoryColumn; direction: "asc" | "desc" };
type HistorySortValue = string | number;

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

  return event.state === "pending" ? "Waiting to start" : "Time unavailable";
}

function getHistoryColumnSortValue(event: CurtailmentHistoryEvent, field: HistoryColumn): HistorySortValue {
  switch (field) {
    case "event":
      return `${event.reason} ${event.id}`;
    case "scope":
      return `${event.scopeLabel} ${event.selectedMiners}`;
    case "target":
      return getTargetKw(event);
    case "state":
      return curtailmentEventStateConfigs[event.state].order;
  }
}

function getDefaultHistorySortDirection(field: HistoryColumn): HistorySort["direction"] {
  return field === "target" ? "desc" : "asc";
}

function compareSortValues(left: HistorySortValue, right: HistorySortValue): number {
  if (typeof left === "number" && typeof right === "number") {
    return left - right;
  }

  return collator.compare(String(left), String(right));
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

function sortHistoryEvents(events: CurtailmentHistoryEvent[], currentSort?: HistorySort): CurtailmentHistoryEvent[] {
  return [...events].sort((left, right) => {
    if (!currentSort) {
      return compareStartedAtDesc(left, right);
    }

    const sortComparison = compareSortValues(
      getHistoryColumnSortValue(left, currentSort.field),
      getHistoryColumnSortValue(right, currentSort.field),
    );
    const directionalComparison = currentSort.direction === "asc" ? sortComparison : -sortComparison;

    return directionalComparison || compareStartedAtDesc(left, right);
  });
}

function isActiveStoppableEvent(event: CurtailmentHistoryEvent, activeEventId?: string): boolean {
  return event.id === activeEventId && stoppableEventStates.has(event.state);
}

function isActiveManageableEvent(event: CurtailmentHistoryEvent, activeEventId?: string): boolean {
  return event.id === activeEventId && manageableEventStates.has(event.state);
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

function isCurtailmentEventState(filter: string): filter is CurtailmentEventState {
  return statusFilterOptionIds.has(filter);
}

function normalizeStatusFilters(filters: string[]): CurtailmentEventState[] {
  return filters.filter(isCurtailmentEventState);
}

function getNextHistorySort(previousSort: HistorySort | undefined, field: HistoryColumn): HistorySort {
  if (previousSort?.field !== field) {
    return { field, direction: getDefaultHistorySortDirection(field) };
  }

  return { field, direction: previousSort.direction === "asc" ? "desc" : "asc" };
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
  onStop,
  stopDisabled,
}: CurtailmentSummaryModalProps): ReactElement {
  const startedAt = formatDateTime(event.startedAt);
  const endedAt = formatDateTime(event.endedAt);
  const scheduledAt = formatDateTime(event.scheduledAt);
  const createdAt = formatDateTime(event.createdAt);
  const eventStateConfig = curtailmentEventStateConfigs[event.state];
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
          <DetailRow label="Applies to" value={event.scopeLabel} secondary={formatMinerCount(event.selectedMiners)} />
          <DetailRow label="Power target vs actual" value={formatTargetVsActual(event)} />
          <DetailRow label="Status" value={eventStateConfig.label} secondary={getHistoryStatusDetail(event)} />
          <DetailRow label="Started" value={startedAt ?? "Not started yet"} />
          {scheduledAt ? <DetailRow label="Scheduled" value={scheduledAt} /> : null}
          {createdAt ? <DetailRow label="Created" value={createdAt} /> : null}
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
  activeEventId,
  onOpenSummary,
  onRequestStop,
  stopDisabled,
}: CurtailmentHistoryRowProps): ReactElement {
  const canStop = Boolean(onRequestStop) && isActiveStoppableEvent(event, activeEventId);
  const eventStateConfig = curtailmentEventStateConfigs[event.state];

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
        <div className="text-200 text-text-primary-50">{formatMinerCount(event.selectedMiners)}</div>
      </td>
      <td className="py-4 pr-6 align-top text-text-primary">{formatTargetVsActual(event)}</td>
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
  pageSize = defaultPageSize,
  className,
  onViewEvent,
  onManageActiveEvent,
  onStopActiveEvent,
}: CurtailmentHistoryProps): ReactElement {
  const [selectedStatusFilters, setSelectedStatusFilters] = useState<CurtailmentEventState[]>([]);
  const [currentSort, setCurrentSort] = useState<HistorySort | undefined>();
  const [currentPage, setCurrentPage] = useState(0);
  const [selectedDetailEventId, setSelectedDetailEventId] = useState<string>();
  const [selectedStopEventId, setSelectedStopEventId] = useState<string>();
  const [pendingStopEventId, setPendingStopEventId] = useState<string>();
  const normalizedPageSize = getNormalizedPageSize(pageSize);
  const hasActiveFilters = selectedStatusFilters.length > 0;
  const selectedDetailEvent = useMemo(
    () => events.find((event) => event.id === selectedDetailEventId),
    [events, selectedDetailEventId],
  );
  const selectedStopEvent = useMemo(
    () => events.find((event) => event.id === selectedStopEventId),
    [events, selectedStopEventId],
  );
  const selectedStopEventIsStoppable = Boolean(
    selectedStopEvent && isActiveStoppableEvent(selectedStopEvent, activeEventId),
  );
  const pendingStopEvent = useMemo(
    () => events.find((event) => event.id === pendingStopEventId),
    [events, pendingStopEventId],
  );
  const pendingStopEventIsStoppable = Boolean(
    pendingStopEvent && isActiveStoppableEvent(pendingStopEvent, activeEventId),
  );
  const pendingStoppableEventId = pendingStopEventIsStoppable ? pendingStopEventId : undefined;

  const filteredEvents = useMemo(() => {
    if (selectedStatusFilters.length === 0) {
      return events;
    }

    const filterSet = new Set(selectedStatusFilters);
    return events.filter((event) => filterSet.has(event.state));
  }, [events, selectedStatusFilters]);

  const sortedEvents = useMemo(() => sortHistoryEvents(filteredEvents, currentSort), [filteredEvents, currentSort]);
  const totalEvents = sortedEvents.length;
  const pageCount = Math.max(Math.ceil(totalEvents / normalizedPageSize), 1);
  const effectiveCurrentPage = Math.min(currentPage, pageCount - 1);
  const firstVisibleEventIndex = effectiveCurrentPage * normalizedPageSize;
  const visibleEvents = sortedEvents.slice(firstVisibleEventIndex, firstVisibleEventIndex + normalizedPageSize);
  const firstItemIndex = firstVisibleEventIndex + 1;
  const lastItemIndex = firstItemIndex + visibleEvents.length - 1;
  const shouldRenderPagination = totalEvents > 0;
  const hasPreviousPage = effectiveCurrentPage > 0;
  const hasNextPage = lastItemIndex < totalEvents;

  const handleStatusFilterChange = (filters: string[]) => {
    setSelectedStatusFilters(normalizeStatusFilters(filters));
    setCurrentPage(0);
  };

  const handleClearStatusFilters = () => handleStatusFilterChange([]);

  const handleSort = (field: HistoryColumn) => {
    setCurrentSort((previousSort) => getNextHistorySort(previousSort, field));
    setCurrentPage(0);
  };

  const handlePageChange = (pageDelta: number) =>
    setCurrentPage((previousPage) => Math.min(Math.max(previousPage + pageDelta, 0), pageCount - 1));

  const handlePreviousPage = () => handlePageChange(-1);

  const handleNextPage = () => handlePageChange(1);

  const handleOpenSummary = (event: CurtailmentHistoryEvent) => {
    setSelectedDetailEventId(event.id);
    onViewEvent?.(event);
  };

  const handleDismissSummary = () => setSelectedDetailEventId(undefined);

  const handleOpenStopConfirmation = (event: CurtailmentHistoryEvent) => {
    if (!onStopActiveEvent || !isActiveStoppableEvent(event, activeEventId) || pendingStoppableEventId === event.id) {
      return;
    }

    setSelectedStopEventId(event.id);
  };

  const handleDismissStopConfirmation = () => setSelectedStopEventId(undefined);

  const handleConfirmStop = () => {
    if (!selectedStopEvent || !onStopActiveEvent || !isActiveStoppableEvent(selectedStopEvent, activeEventId)) {
      return;
    }

    const event = selectedStopEvent;
    setSelectedStopEventId(undefined);
    setPendingStopEventId(event.id);

    let stopRequest: void | PromiseLike<unknown>;
    try {
      stopRequest = onStopActiveEvent(event);
    } catch {
      setPendingStopEventId((currentEventId) => (currentEventId === event.id ? undefined : currentEventId));
      return;
    }

    if (!isPromiseLike(stopRequest)) {
      return;
    }

    void Promise.resolve(stopRequest).catch(() => {
      setPendingStopEventId((currentEventId) => (currentEventId === event.id ? undefined : currentEventId));
    });
  };

  const handleManageSelectedDetailEvent =
    selectedDetailEvent &&
    onManageActiveEvent &&
    isActiveManageableEvent(selectedDetailEvent, activeEventId) &&
    selectedDetailEvent.id !== pendingStoppableEventId
      ? () => {
          setSelectedDetailEventId(undefined);
          onManageActiveEvent(selectedDetailEvent);
        }
      : undefined;

  const handleStopSelectedDetailEvent =
    selectedDetailEvent && onStopActiveEvent && isActiveStoppableEvent(selectedDetailEvent, activeEventId)
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
                  <button
                    type="button"
                    className="flex items-center gap-1 text-left text-emphasis-300 text-text-primary-50 hover:text-text-primary"
                    onClick={() => handleSort(column)}
                  >
                    {historyColumnLabels[column]}
                    {currentSort?.field === column ? (
                      <ChevronDown
                        aria-hidden="true"
                        className={clsx("transition-transform", {
                          "rotate-180": currentSort.direction === "asc",
                        })}
                        width={iconSizes.xSmall}
                      />
                    ) : null}
                  </button>
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
                activeEventId={activeEventId}
                onOpenSummary={handleOpenSummary}
                onRequestStop={onStopActiveEvent ? handleOpenStopConfirmation : undefined}
                stopDisabled={event.id === pendingStoppableEventId}
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
        <div className="flex flex-col items-center gap-4 pt-6 pb-6" data-testid="curtailment-history-pagination">
          <span className="text-300 text-text-primary">
            Showing {firstItemIndex}-{lastItemIndex} of {totalEvents} curtailment events
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
          onStop={handleStopSelectedDetailEvent}
          stopDisabled={selectedDetailEvent.id === pendingStoppableEventId}
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
