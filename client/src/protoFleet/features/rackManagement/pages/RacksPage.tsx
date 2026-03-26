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
import RackSettingsModal from "@/protoFleet/features/rackManagement/components/RackSettingsModal";
import { useCollectionListState } from "@/protoFleet/hooks/useCollectionListState";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

import { DismissTiny, Racks } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";

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
    error,
    currentSort,
    currentPage,
    hasNextPage,
    totalCount,
    handleSort,
    handleNextPage,
    handlePrevPage,
    resetAndFetch,
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
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <SegmentedControl
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
        <div className="p-10 pt-0 phone:p-6 phone:pt-0 tablet:p-6 tablet:pt-0">
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
        <div className="grid grid-cols-1 gap-4 px-10 sm:grid-cols-2 lg:grid-cols-3 phone:px-6 tablet:px-6">
          {racks.map((rack) => (
            <button
              key={rack.id.toString()}
              type="button"
              className="cursor-pointer rounded-xl border border-border-10 bg-surface-base p-4 text-left hover:bg-surface-5"
              onClick={() => handleOpenRackForEdit(rack)}
            >
              <p className="text-heading-100 text-text-primary">{rack.label}</p>
              <p className="text-200 text-text-primary-50">{rack.description}</p>
            </button>
          ))}
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
