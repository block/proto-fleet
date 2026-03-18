import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";

import { useCollections } from "@/protoFleet/api/useCollections";
import type { CollectionListItem } from "@/protoFleet/components/CollectionList";
import {
  CollectionList,
  DEFAULT_PAGE_SIZE,
  issueOptions,
  useIssueFilter,
} from "@/protoFleet/components/CollectionList";
import { useCollectionListState } from "@/protoFleet/hooks/useCollectionListState";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

import { DismissTiny, Racks } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import DropdownFilter from "@/shared/components/List/Filters/DropdownFilter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";

const RacksPage = () => {
  const { listRacks, listRackLocations } = useCollections();
  const [selectedLocations, setSelectedLocations] = useState<string[]>([]);
  const [selectedIssues, setSelectedIssues] = useState<string[]>([]);
  const [allLocations, setAllLocations] = useState<{ id: string; label: string }[]>([]);

  const { selectedIssuesRef, getErrorComponentTypes } = useIssueFilter();

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
  } = useCollectionListState(listRacks, DEFAULT_PAGE_SIZE, getErrorComponentTypes);

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

  // Client-side location filter (applied on display only)
  const filteredRacks = useMemo(() => {
    if (selectedLocations.length === 0) return racks;
    return racks.filter((rack) => {
      if (rack.typeDetails.case !== "rackInfo") return false;
      return (
        rack.typeDetails.value.location !== undefined && selectedLocations.includes(rack.typeDetails.value.location)
      );
    });
  }, [racks, selectedLocations]);

  const handleIssuesChange = useCallback(
    (issues: string[]) => {
      setSelectedIssues(issues);
      selectedIssuesRef.current = issues;
      resetAndFetch();
    },
    [resetAndFetch, selectedIssuesRef],
  );

  const handleLocationsChange = useCallback((locations: string[]) => {
    setSelectedLocations(locations);
  }, []);

  const handleRemoveLocation = useCallback((locationId: string) => {
    setSelectedLocations((prev) => prev.filter((id) => id !== locationId));
  }, []);

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

  const renderName = useCallback(
    (item: CollectionListItem) => (
      <Link to={`/racks/${item.collection.id}`} className="hover:underline">
        {item.collection.label}
      </Link>
    ),
    [],
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
        <p className="text-body-200 text-text-secondary">{error}</p>
      </div>
    );
  }

  const hasRacks = racks.length > 0 || hasEverLoaded;

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
              <Button variant="primary" onClick={() => {}}>
                Add rack
              </Button>
            </div>
          </div>
        </div>
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
              <Button variant={variants.secondary} size={sizes.compact} onClick={() => {}}>
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
        <div className="text-body-200 text-intent-critical mx-10 mb-4 rounded-lg bg-intent-critical-10 px-4 py-3 phone:mx-6 tablet:mx-6">
          {error}
        </div>
      )}
      {racksViewMode === "list" ? (
        <div className="p-10 pt-0 phone:p-6 phone:pt-0 tablet:p-6 tablet:pt-0">
          <CollectionList
            collections={filteredRacks}
            statsMap={statsMap}
            renderName={renderName}
            renderMiners={renderMiners}
            currentSort={currentSort}
            onSort={handleSort}
            itemName={{ singular: "rack", plural: "racks" }}
            total={selectedLocations.length > 0 ? filteredRacks.length : totalCount}
            loading={isLoading}
            pageSize={DEFAULT_PAGE_SIZE}
            currentPage={currentPage}
            hasPreviousPage={currentPage > 0}
            hasNextPage={selectedLocations.length > 0 ? false : hasNextPage}
            onNextPage={handleNextPage}
            onPrevPage={handlePrevPage}
          />
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 px-10 sm:grid-cols-2 lg:grid-cols-3 phone:px-6 tablet:px-6">
          {filteredRacks.map((rack) => (
            <div key={rack.id.toString()} className="border-border-secondary bg-surface-primary rounded-xl border p-4">
              <p className="text-heading-100 text-text-primary">{rack.label}</p>
              <p className="text-body-100 text-text-secondary">{rack.description}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default RacksPage;
