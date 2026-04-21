import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import type { DeviceSetListItem } from "@/protoFleet/components/DeviceSetList";
import type { DeviceSetColumn } from "@/protoFleet/components/DeviceSetList";
import { DEFAULT_PAGE_SIZE, DeviceSetList, issueOptions, useIssueFilter } from "@/protoFleet/components/DeviceSetList";
import { getNextSortFromSelection, RACK_SORT_OPTIONS } from "@/protoFleet/components/DeviceSetList/sortConfig";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import {
  AssignMinersModal,
  type RackFormData,
} from "@/protoFleet/features/rackManagement/components/AssignMinersModal";
import { RackCard } from "@/protoFleet/features/rackManagement/components/RackCard";
import RackSettingsModal from "@/protoFleet/features/rackManagement/components/RackSettingsModal";
import { mapRackToCardProps } from "@/protoFleet/features/rackManagement/utils/rackCardMapper";
import { useDeviceSetListState } from "@/protoFleet/hooks/useDeviceSetListState";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

import { Alert, ChevronDown, DismissTiny, Racks } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import useMeasure from "@/shared/hooks/useMeasure";
import { useNavigate } from "@/shared/hooks/useNavigate";

const RACK_COLUMNS: DeviceSetColumn[] = [
  "name",
  "zone",
  "miners",
  "issues",
  "hashrate",
  "efficiency",
  "power",
  "temperature",
  "health",
];

const RacksPage = () => {
  const navigate = useNavigate();
  const { listRacks, listRackZones, deleteGroup } = useDeviceSets();
  const [showRackSettingsModal, setShowRackSettingsModal] = useState(false);
  const [selectedZones, setSelectedZones] = useState<string[]>([]);
  const [selectedIssues, setSelectedIssues] = useState<string[]>([]);
  const [allZones, setAllZones] = useState<{ id: string; label: string }[]>([]);

  // AssignMinersModal state
  const [assignMinersFormData, setAssignMinersFormData] = useState<RackFormData | null>(null);
  const [assignMinersRackId, setAssignMinersRackId] = useState<bigint | undefined>(undefined);

  const { selectedIssuesRef, getErrorComponentTypes } = useIssueFilter();

  const selectedZonesRef = useRef<string[]>([]);
  const getZones = useCallback(() => selectedZonesRef.current, []);

  const {
    deviceSets: racks,
    statsMap,
    isLoading,
    hasEverLoaded,
    hasCompletedInitialFetch,
    error,
    currentSort,
    currentPage,
    hasNextPage,
    totalCount,
    handleSort,
    handleNextPage,
    handlePrevPage,
    resetAndFetch,
    refreshCurrentPage,
  } = useDeviceSetListState(listRacks, DEFAULT_PAGE_SIZE, getErrorComponentTypes, getZones);

  const racksViewMode = useFleetStore((s) => s.ui.racksViewMode);
  const setRacksViewMode = useFleetStore((s) => s.ui.setRacksViewMode);
  const temperatureUnit = useFleetStore((s) => s.ui.temperatureUnit);

  // Fetch all rack zones once on mount
  const zonesRequestId = useRef(0);
  const fetchZones = useCallback(() => {
    const requestId = ++zonesRequestId.current;
    listRackZones({
      onSuccess: (zones) => {
        if (requestId !== zonesRequestId.current) return;
        setAllZones(zones.map((z) => ({ id: z, label: z })));
      },
    });
  }, [listRackZones]);

  useEffect(() => {
    fetchZones();
  }, [fetchZones]);

  const handleIssuesChange = useCallback(
    (issues: string[]) => {
      setSelectedIssues(issues);
      selectedIssuesRef.current = issues;
      resetAndFetch();
    },
    [resetAndFetch, selectedIssuesRef],
  );

  const handleZonesChange = useCallback(
    (zones: string[]) => {
      setSelectedZones(zones);
      selectedZonesRef.current = zones;
      resetAndFetch();
    },
    [resetAndFetch],
  );

  const handleRemoveZone = useCallback(
    (zoneId: string) => {
      const next = selectedZones.filter((id) => id !== zoneId);
      setSelectedZones(next);
      selectedZonesRef.current = next;
      resetAndFetch();
    },
    [selectedZones, resetAndFetch],
  );

  const handleRemoveIssue = useCallback(
    (issueId: string) => {
      const next = selectedIssues.filter((id) => id !== issueId);
      setSelectedIssues(next);
      selectedIssuesRef.current = next;
      resetAndFetch();
    },
    [selectedIssues, resetAndFetch, selectedIssuesRef],
  );

  const activeFilterPills = useMemo(() => {
    const pills: { key: string; label: string; type: "zone" | "issue"; id: string }[] = [];
    for (const zoneId of selectedZones) {
      const z = allZones.find((l) => l.id === zoneId);
      if (z) {
        pills.push({ key: `zone-${zoneId}`, label: z.label, type: "zone", id: zoneId });
      }
    }
    for (const issueId of selectedIssues) {
      const issue = issueOptions.find((o) => o.id === issueId);
      if (issue) {
        pills.push({ key: `issue-${issueId}`, label: issue.label, type: "issue", id: issueId });
      }
    }
    return pills;
  }, [selectedZones, selectedIssues, allZones]);

  const hasActiveFilters = selectedZones.length > 0 || selectedIssues.length > 0;

  const handleClearFilters = useCallback(() => {
    setSelectedZones([]);
    selectedZonesRef.current = [];
    setSelectedIssues([]);
    selectedIssuesRef.current = [];
    resetAndFetch();
  }, [resetAndFetch, selectedIssuesRef]);

  const emptyStateRow: ReactNode = useMemo(() => {
    if (isLoading || totalCount > 0) return undefined;
    return <NoFilterResultsEmptyState hasActiveFilters={hasActiveFilters} onClearFilters={handleClearFilters} />;
  }, [hasActiveFilters, isLoading, totalCount, handleClearFilters]);

  const handleRackSettingsContinue = useCallback((formData: RackFormData) => {
    setShowRackSettingsModal(false);
    setAssignMinersFormData(formData);
    setAssignMinersRackId(undefined);
  }, []);

  const handleAssignMinersDismiss = useCallback(() => {
    setAssignMinersFormData(null);
    setAssignMinersRackId(undefined);
  }, []);

  const handleAssignMinersSave = useCallback(() => {
    setAssignMinersFormData(null);
    setAssignMinersRackId(undefined);
    resetAndFetch();
    fetchZones();
  }, [resetAndFetch, fetchZones]);

  const handleDeleteRack = useCallback(() => {
    if (!assignMinersRackId) return Promise.resolve();
    return new Promise<void>((resolve, reject) => {
      deleteGroup({
        deviceSetId: assignMinersRackId,
        onSuccess: () => {
          pushToast({ message: "Rack deleted", status: STATUSES.success });
          setAssignMinersFormData(null);
          setAssignMinersRackId(undefined);
          resetAndFetch();
          fetchZones();
          resolve();
        },
        onError: (msg) => {
          pushToast({ message: msg, status: STATUSES.error });
          reject(new Error(msg));
        },
      });
    });
  }, [assignMinersRackId, deleteGroup, resetAndFetch, fetchZones]);

  const renderName = useCallback(
    (item: DeviceSetListItem) => (
      <button
        type="button"
        className="text-left hover:underline"
        onClick={() => navigate(`/racks/${item.deviceSet.id}`)}
      >
        {item.deviceSet.label}
      </button>
    ),
    [navigate],
  );

  const renderMiners = useCallback((item: DeviceSetListItem) => <span>{item.deviceSet.deviceCount}</span>, []);

  // Responsive grid measurement
  const [measureRef, contentRect] = useMeasure<HTMLDivElement>();
  const RACK_CARD_MIN_WIDTH_PX = 300;
  const numColumns = Math.max(1, Math.floor((contentRect.width || RACK_CARD_MIN_WIDTH_PX) / RACK_CARD_MIN_WIDTH_PX));

  // Polling — refresh current page every 60s, paused while modals are open
  const isModalOpen = !!assignMinersFormData || showRackSettingsModal;
  useEffect(() => {
    if (!hasCompletedInitialFetch || isModalOpen) return;
    const intervalId = setInterval(() => {
      refreshCurrentPage();
    }, POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [hasCompletedInitialFetch, isModalOpen, refreshCurrentPage]);

  // Sort dropdown handler for grid view
  const handleSortSelect = useCallback(
    (selected: string[]) => {
      const nextSort = getNextSortFromSelection(selected, currentSort);
      handleSort(nextSort.field, nextSort.direction);
    },
    [currentSort, handleSort],
  );

  // Grid pagination
  const firstItemIndex = currentPage * DEFAULT_PAGE_SIZE + 1;
  const lastItemIndex = currentPage * DEFAULT_PAGE_SIZE + racks.length;
  const shouldRenderGridPagination = !isLoading && totalCount > 0;

  if (isLoading && !hasEverLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (error && !hasEverLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-300 text-text-primary-50">{error}</p>
      </div>
    );
  }

  const hasRacks = hasEverLoaded || totalCount > 0 || racks.length > 0;

  if (!hasRacks) {
    return (
      <div className="flex h-full flex-col justify-center p-6 sm:p-10">
        <div className="flex h-full w-full flex-col justify-center rounded-xl bg-surface-5 px-6 py-10 sm:px-20 sm:py-10 dark:bg-surface-base">
          <div className="flex flex-col gap-6">
            <div className="flex flex-col gap-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-core-primary-5">
                <Racks width="w-5" />
              </div>
              <Header
                title="You haven't set up any racks"
                titleSize="text-display-200"
                description="Add a rack and assign miners to rack positions to get started."
              />
            </div>
            <div>
              <Button variant="primary" onClick={() => setShowRackSettingsModal(true)}>
                Add rack
              </Button>
            </div>
          </div>
        </div>
        {showRackSettingsModal && (
          <RackSettingsModal
            show={showRackSettingsModal}
            existingRacks={racks}
            onDismiss={() => setShowRackSettingsModal(false)}
            onContinue={handleRackSettingsContinue}
          />
        )}
        {assignMinersFormData && (
          <AssignMinersModal
            show={!!assignMinersFormData}
            rackSettings={assignMinersFormData}
            existingRackId={assignMinersRackId}
            existingRacks={racks}
            onDismiss={handleAssignMinersDismiss}
            onSave={handleAssignMinersSave}
            onDelete={assignMinersRackId ? handleDeleteRack : undefined}
          />
        )}
      </div>
    );
  }

  return (
    <div>
      <div className="sticky left-0 z-3 px-10 pt-10 phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
        <h1 className="pb-4 text-heading-300 text-text-primary">Racks</h1>
        <div className="flex flex-col gap-2 pb-6">
          {/* Action button — full-width on tablet/phone */}
          <div className="hidden phone:block tablet:block">
            <Button variant={variants.secondary} size={sizes.compact} onClick={() => setShowRackSettingsModal(true)}>
              Add rack
            </Button>
          </div>
          {/* View toggle — full width on tablet/phone */}
          <div className="hidden phone:block tablet:block">
            <SegmentedControl
              key={`mobile-${racksViewMode}`}
              className="!w-full whitespace-nowrap [&>button]:flex-1"
              segmentClassName="text-center"
              segments={[
                { key: "grid", title: "View grid" },
                { key: "list", title: "View list" },
              ]}
              initialSegmentKey={racksViewMode}
              onSelect={(key) => setRacksViewMode(key as "grid" | "list")}
            />
          </div>
          {/* Desktop layout — single row with toggle + filters left, buttons right */}
          <div className="flex items-center justify-between gap-2 phone:hidden tablet:hidden">
            <div className="flex items-center gap-2">
              <SegmentedControl
                key={`desktop-${racksViewMode}`}
                className="shrink-0 whitespace-nowrap"
                segments={[
                  { key: "grid", title: "View grid" },
                  { key: "list", title: "View list" },
                ]}
                initialSegmentKey={racksViewMode}
                onSelect={(key) => setRacksViewMode(key as "grid" | "list")}
              />
              <DropdownFilter
                title="Zone"
                options={allZones}
                selectedOptions={selectedZones}
                onSelect={handleZonesChange}
                withButtons
              />
              <DropdownFilter
                title="Issues"
                options={issueOptions}
                selectedOptions={selectedIssues}
                onSelect={handleIssuesChange}
                withButtons
              />
              {racksViewMode === "grid" && (
                <DropdownFilter
                  title="Sort"
                  options={RACK_SORT_OPTIONS}
                  selectedOptions={[currentSort.field]}
                  onSelect={handleSortSelect}
                  showSelectAll={false}
                />
              )}
            </div>
            <Button variant={variants.secondary} size={sizes.compact} onClick={() => setShowRackSettingsModal(true)}>
              Add rack
            </Button>
          </div>
          {/* Filters — shown separately on tablet/phone */}
          <div className="hidden items-center gap-2 phone:flex tablet:flex">
            <DropdownFilter
              title="Zone"
              options={allZones}
              selectedOptions={selectedZones}
              onSelect={handleZonesChange}
              withButtons
            />
            <DropdownFilter
              title="Issues"
              options={issueOptions}
              selectedOptions={selectedIssues}
              onSelect={handleIssuesChange}
              withButtons
            />
            {racksViewMode === "grid" && (
              <DropdownFilter
                title="Sort"
                options={RACK_SORT_OPTIONS}
                selectedOptions={[currentSort.field]}
                onSelect={handleSortSelect}
                showSelectAll={false}
              />
            )}
          </div>
          {activeFilterPills.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {activeFilterPills.map((pill) => (
                <Button
                  key={pill.key}
                  size={sizes.compact}
                  variant={variants.accent}
                  prefixIcon={<DismissTiny />}
                  onClick={() => (pill.type === "zone" ? handleRemoveZone(pill.id) : handleRemoveIssue(pill.id))}
                >
                  {pill.label}
                </Button>
              ))}
            </div>
          )}
        </div>
      </div>
      {error ? (
        <Callout className="mx-10 mb-4 phone:mx-6 tablet:mx-6" intent="danger" prefixIcon={<Alert />} title={error} />
      ) : null}
      {racksViewMode === "list" ? (
        <div className="overflow-x-auto p-10 pt-0 phone:p-6 phone:pt-0 tablet:p-6 tablet:pt-0">
          <DeviceSetList
            deviceSets={racks}
            statsMap={statsMap}
            renderName={renderName}
            renderMiners={renderMiners}
            columns={RACK_COLUMNS}
            currentSort={currentSort}
            onSort={handleSort}
            itemName={{ singular: "rack", plural: "racks" }}
            total={totalCount}
            loading={isLoading}
            pageSize={DEFAULT_PAGE_SIZE}
            currentPage={currentPage}
            hasPreviousPage={currentPage > 0}
            hasNextPage={hasNextPage}
            onNextPage={handleNextPage}
            onPrevPage={handlePrevPage}
            emptyStateRow={emptyStateRow}
          />
        </div>
      ) : (
        <div className="px-10 phone:px-6 tablet:px-6">
          {isLoading && racks.length === 0 ? (
            <div className="flex items-center justify-center py-20">
              <ProgressCircular indeterminate />
            </div>
          ) : racks.length === 0 ? (
            <NoFilterResultsEmptyState hasActiveFilters={hasActiveFilters} onClearFilters={handleClearFilters} />
          ) : (
            <div ref={measureRef}>
              <div className="grid gap-1" style={{ gridTemplateColumns: `repeat(${numColumns}, 1fr)` }}>
                {racks.map((rack) => {
                  const stats = statsMap.get(rack.id);
                  const { zone, rows, cols, loading, statusSegments, slots, hashrate, efficiency, power, temperature } =
                    mapRackToCardProps(rack, stats, temperatureUnit);
                  return (
                    <RackCard
                      key={rack.id.toString()}
                      label={rack.label}
                      zone={zone}
                      cols={cols}
                      rows={rows}
                      slots={slots}
                      loading={loading}
                      statusSegments={statusSegments}
                      hashrate={hashrate}
                      efficiency={efficiency}
                      power={power}
                      temperature={temperature}
                      onClick={() => navigate(`/racks/${rack.id}`)}
                    />
                  );
                })}
              </div>
            </div>
          )}
          {(shouldRenderGridPagination || (currentPage > 0 && racks.length === 0)) && (
            <div className="sticky left-0 flex flex-col items-center gap-4 py-6">
              <span className="text-300 text-text-primary">
                Showing {firstItemIndex}–{lastItemIndex} of {totalCount} racks
              </span>
              <div className="flex gap-3">
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  ariaLabel="Previous page"
                  prefixIcon={<ChevronDown className="rotate-90" />}
                  onClick={handlePrevPage}
                  disabled={currentPage === 0}
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
          )}
        </div>
      )}
      {showRackSettingsModal && (
        <RackSettingsModal
          show={showRackSettingsModal}
          existingRacks={racks}
          onDismiss={() => setShowRackSettingsModal(false)}
          onContinue={handleRackSettingsContinue}
        />
      )}
      {assignMinersFormData && (
        <AssignMinersModal
          show={!!assignMinersFormData}
          rackSettings={assignMinersFormData}
          existingRackId={assignMinersRackId}
          existingRacks={racks}
          onDismiss={handleAssignMinersDismiss}
          onSave={handleAssignMinersSave}
        />
      )}
    </div>
  );
};

export default RacksPage;
