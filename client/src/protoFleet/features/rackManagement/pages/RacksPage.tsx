import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  type DeviceCollection,
  RackCoolingType,
  RackOrderIndex,
} from "@/protoFleet/api/generated/collection/v1/collection_pb";
import { useCollections } from "@/protoFleet/api/useCollections";
import type { CollectionListItem } from "@/protoFleet/components/CollectionList";
import type { CollectionColumn } from "@/protoFleet/components/CollectionList";
import {
  CollectionList,
  DEFAULT_PAGE_SIZE,
  issueOptions,
  useIssueFilter,
} from "@/protoFleet/components/CollectionList";
import {
  AssignMinersModal,
  type RackFormData,
} from "@/protoFleet/features/rackManagement/components/AssignMinersModal";
import { RackCard } from "@/protoFleet/features/rackManagement/components/RackCard";
import RackSettingsModal from "@/protoFleet/features/rackManagement/components/RackSettingsModal";
import { mapRackToCardProps } from "@/protoFleet/features/rackManagement/utils/rackCardMapper";
import { useCollectionListState } from "@/protoFleet/hooks/useCollectionListState";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

import { ChevronDown, DismissTiny, Racks } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";
import useMeasure from "@/shared/hooks/useMeasure";

const RACK_POLL_INTERVAL_MS = Number(import.meta.env.VITE_RACK_LIST_POLL_INTERVAL_MS) || 60000;

const SORT_OPTIONS: { id: string; label: string }[] = [
  { id: "name", label: "Name" },
  { id: "location", label: "Location" },
  { id: "miners", label: "Miners" },
];

const RACK_COLUMNS: CollectionColumn[] = [
  "name",
  "location",
  "miners",
  "issues",
  "hashrate",
  "efficiency",
  "power",
  "temperature",
  "health",
];

const RacksPage = () => {
  const { listRacks, listRackLocations } = useCollections();
  const [showRackSettingsModal, setShowRackSettingsModal] = useState(false);
  const [selectedLocations, setSelectedLocations] = useState<string[]>([]);
  const [selectedIssues, setSelectedIssues] = useState<string[]>([]);
  const [allLocations, setAllLocations] = useState<{ id: string; label: string }[]>([]);

  // AssignMinersModal state
  const [assignMinersFormData, setAssignMinersFormData] = useState<RackFormData | null>(null);
  const [assignMinersRackId, setAssignMinersRackId] = useState<bigint | undefined>(undefined);

  const { selectedIssuesRef, getErrorComponentTypes } = useIssueFilter();

  const selectedLocationsRef = useRef<string[]>([]);
  const getLocations = useCallback(() => selectedLocationsRef.current, []);

  const {
    collections: racks,
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
  } = useCollectionListState(listRacks, DEFAULT_PAGE_SIZE, getErrorComponentTypes, getLocations);

  const racksViewMode = useFleetStore((s) => s.ui.racksViewMode);
  const setRacksViewMode = useFleetStore((s) => s.ui.setRacksViewMode);

  // Fetch all rack locations once on mount
  const locationsRequestId = useRef(0);
  const fetchLocations = useCallback(() => {
    const requestId = ++locationsRequestId.current;
    listRackLocations({
      onSuccess: (locations) => {
        if (requestId !== locationsRequestId.current) return;
        setAllLocations(locations.map((loc) => ({ id: loc, label: loc })));
      },
    });
  }, [listRackLocations]);

  useEffect(() => {
    fetchLocations();
  }, [fetchLocations]);

  const handleIssuesChange = useCallback(
    (issues: string[]) => {
      setSelectedIssues(issues);
      selectedIssuesRef.current = issues;
      resetAndFetch();
    },
    [resetAndFetch, selectedIssuesRef],
  );

  const handleLocationsChange = useCallback(
    (locations: string[]) => {
      setSelectedLocations(locations);
      selectedLocationsRef.current = locations;
      resetAndFetch();
    },
    [resetAndFetch],
  );

  const handleRemoveLocation = useCallback(
    (locationId: string) => {
      const next = selectedLocations.filter((id) => id !== locationId);
      setSelectedLocations(next);
      selectedLocationsRef.current = next;
      resetAndFetch();
    },
    [selectedLocations, resetAndFetch],
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
    const pills: { key: string; label: string; type: "location" | "issue"; id: string }[] = [];
    for (const locId of selectedLocations) {
      const loc = allLocations.find((l) => l.id === locId);
      if (loc) {
        pills.push({ key: `loc-${locId}`, label: loc.label, type: "location", id: locId });
      }
    }
    for (const issueId of selectedIssues) {
      const issue = issueOptions.find((o) => o.id === issueId);
      if (issue) {
        pills.push({ key: `issue-${issueId}`, label: issue.label, type: "issue", id: issueId });
      }
    }
    return pills;
  }, [selectedLocations, selectedIssues, allLocations]);

  const handleOpenRackForEdit = useCallback((rack: DeviceCollection) => {
    const rackInfo = rack.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;
    setAssignMinersFormData({
      label: rack.label,
      location: rackInfo?.location ?? "",
      rows: rackInfo?.rows ?? 1,
      columns: rackInfo?.columns ?? 1,
      orderIndex: rackInfo?.orderIndex ?? RackOrderIndex.BOTTOM_LEFT,
      coolingType: rackInfo?.coolingType ?? RackCoolingType.AIR,
    });
    setAssignMinersRackId(rack.id);
  }, []);

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
    fetchLocations();
  }, [resetAndFetch, fetchLocations]);

  const renderName = useCallback(
    (item: CollectionListItem) => (
      <button
        type="button"
        className="text-left hover:underline"
        onClick={() => handleOpenRackForEdit(item.collection)}
      >
        {item.collection.label}
      </button>
    ),
    [handleOpenRackForEdit],
  );

  const renderMiners = useCallback((item: CollectionListItem) => <span>{item.collection.deviceCount}</span>, []);

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
    }, RACK_POLL_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [hasCompletedInitialFetch, isModalOpen, refreshCurrentPage]);

  // Sort dropdown handler for grid view
  const handleSortSelect = useCallback(
    (selected: string[]) => {
      if (selected.length === 0) {
        // User deselected the current field — toggle direction
        const direction = currentSort.direction === "asc" ? "desc" : "asc";
        handleSort(currentSort.field, direction);
        return;
      }
      // DropdownFilter is multi-select — find the newly toggled field
      const newField = selected.find((s) => s !== currentSort.field) ?? selected[0];
      const field = newField as CollectionColumn;
      const direction = field === currentSort.field && currentSort.direction === "asc" ? "desc" : "asc";
      handleSort(field, direction);
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
          {/* Action buttons — bottom-right on desktop, top full-width on tablet/phone */}
          <div className="hidden grid-cols-2 gap-2 phone:grid tablet:grid">
            <Button variant={variants.secondary} size={sizes.compact} onClick={() => setShowRackSettingsModal(true)}>
              Add rack
            </Button>
            <Button variant={variants.secondary} size={sizes.compact} onClick={() => {}}>
              Add multiple racks
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
                title="Location"
                options={allLocations}
                selectedOptions={selectedLocations}
                onSelect={handleLocationsChange}
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
                  options={SORT_OPTIONS}
                  selectedOptions={[currentSort.field]}
                  onSelect={handleSortSelect}
                />
              )}
            </div>
            <div className="flex items-center gap-2">
              <Button variant={variants.secondary} size={sizes.compact} onClick={() => setShowRackSettingsModal(true)}>
                Add rack
              </Button>
              <Button variant={variants.secondary} size={sizes.compact} onClick={() => {}}>
                Add multiple racks
              </Button>
            </div>
          </div>
          {/* Filters — shown separately on tablet/phone */}
          <div className="hidden items-center gap-2 phone:flex tablet:flex">
            <DropdownFilter
              title="Location"
              options={allLocations}
              selectedOptions={selectedLocations}
              onSelect={handleLocationsChange}
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
                options={SORT_OPTIONS}
                selectedOptions={[currentSort.field]}
                onSelect={handleSortSelect}
              />
            )}
          </div>
          {activeFilterPills.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {activeFilterPills.map((pill) => (
                <Button
                  key={pill.key}
                  size={sizes.compact}
                  variant={variants.secondary}
                  prefixIcon={<DismissTiny />}
                  onClick={() =>
                    pill.type === "location" ? handleRemoveLocation(pill.id) : handleRemoveIssue(pill.id)
                  }
                >
                  {pill.label}
                </Button>
              ))}
            </div>
          )}
        </div>
      </div>
      {error && (
        <div className="text-intent-critical mx-10 mb-4 rounded-lg bg-intent-critical-10 px-4 py-3 text-300 phone:mx-6 tablet:mx-6">
          {error}
        </div>
      )}
      {racksViewMode === "list" ? (
        <div className="overflow-x-auto p-10 pt-0 phone:p-6 phone:pt-0 tablet:p-6 tablet:pt-0">
          <CollectionList
            collections={racks}
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
          />
        </div>
      ) : (
        <div className="px-10 phone:px-6 tablet:px-6">
          {isLoading && racks.length === 0 ? (
            <div className="flex items-center justify-center py-20">
              <ProgressCircular indeterminate />
            </div>
          ) : racks.length === 0 ? (
            <div className="flex items-center justify-center py-20">
              <p className="text-300 text-text-primary-50">No racks match the current filters</p>
            </div>
          ) : (
            <div ref={measureRef}>
              <div className="grid gap-1" style={{ gridTemplateColumns: `repeat(${numColumns}, 1fr)` }}>
                {racks.map((rack) => {
                  const stats = statsMap.get(rack.id);
                  const {
                    building,
                    rows,
                    cols,
                    loading,
                    statusSegments,
                    slots,
                    hashrate,
                    efficiency,
                    power,
                    temperature,
                  } = mapRackToCardProps(rack, stats);
                  return (
                    <RackCard
                      key={rack.id.toString()}
                      label={rack.label}
                      building={building}
                      cols={cols}
                      rows={rows}
                      slots={slots}
                      loading={loading}
                      statusSegments={statusSegments}
                      hashrate={hashrate}
                      efficiency={efficiency}
                      power={power}
                      temperature={temperature}
                      onClick={() => handleOpenRackForEdit(rack)}
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
