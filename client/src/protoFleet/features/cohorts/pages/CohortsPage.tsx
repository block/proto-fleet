import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";

import {
  type CohortDevice,
  CohortDeviceAssignment,
  type CohortSummary,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";
import CohortModal from "@/protoFleet/features/cohorts/components/CohortModal";
import {
  cohortDeviceDisplayName,
  cohortDeviceSecondaryText,
  cohortStateLabel,
  formatCohortTimestamp,
  isActiveNonDefaultCohort,
  isSuperAdminRole,
} from "@/protoFleet/features/cohorts/utils";
import { scopedPath, useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import { useRole, useUsername } from "@/protoFleet/store";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { Alert, ArrowRight, ChevronDown, Plus } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import { type DropdownOption } from "@/shared/components/List/Filters/DropdownFilter";
import FilterChipsBar, { type FilterChipsBarFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Search from "@/shared/components/Search";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

const pluralize = (count: number, singular: string, plural = `${singular}s`) =>
  `${count} ${count === 1 ? singular : plural}`;

type RigAssignmentFilterKey = "assignment" | "cohort" | "owner" | "target" | "site";

const cohortPageSize = 50;
const availableAssignmentValue = "available";
const reservedAssignmentValue = "reserved";
const defaultCohortFilterValue = "__default__";
const unownedFilterValue = "__unowned__";
const unassignedSiteFilterValue = "__unassigned_site__";
const targetFilterSeparator = "\u0000";

const emptyRigAssignmentFilters = (): Record<RigAssignmentFilterKey, string[]> => ({
  assignment: [],
  cohort: [],
  owner: [],
  target: [],
  site: [],
});

const uniqueOptions = (options: DropdownOption[]) =>
  Array.from(new Map(options.map((option) => [option.id, option])).values()).sort((a, b) =>
    a.label.localeCompare(b.label),
  );

const getCohortFilterValue = (device: CohortDevice) =>
  device.effectiveCohort && !device.effectiveCohort.isDefault
    ? device.effectiveCohort.id.toString()
    : defaultCohortFilterValue;

const getOwnerFilterValue = (device: CohortDevice) =>
  device.effectiveCohort?.ownerUserId?.toString() || unownedFilterValue;

const getOwnerLabel = (device: CohortDevice) => device.effectiveCohort?.ownerUsername || "Unowned";

const getSiteFilterValue = (device: CohortDevice) => device.siteId?.toString() || unassignedSiteFilterValue;

const getSiteLabel = (device: CohortDevice) => device.display?.siteLabel || "Unassigned";

const getTargetLabel = (device: CohortDevice) =>
  [device.display?.manufacturer, device.display?.model].filter(Boolean).join(" ") || "Unknown";

const getTargetFilterValue = (device: CohortDevice) => {
  const manufacturer = device.display?.manufacturer?.trim();
  const model = device.display?.model?.trim();
  return manufacturer && model ? `${manufacturer}${targetFilterSeparator}${model}` : "";
};

const bigintFromFilterValue = (value: string) => {
  try {
    return BigInt(value);
  } catch {
    return undefined;
  }
};

const pageEndCount = (pageIndex: number, itemCount: number) => pageIndex * cohortPageSize + itemCount;

const displayedTotalCount = (reportedTotal: number, pageIndex: number, itemCount: number) =>
  Math.max(reportedTotal, pageEndCount(pageIndex, itemCount));

const isOwnedByCurrentUser = (cohort: CohortSummary, username: string) => {
  const ownerUsername = cohort.ownerUsername.trim().toLowerCase();
  const currentUsername = username.trim().toLowerCase();
  return ownerUsername !== "" && currentUsername !== "" && ownerUsername === currentUsername;
};

const CohortsPage = () => {
  const activeSite = useRouteSiteScope() ?? DEFAULT_ACTIVE_SITE;
  const navigate = useNavigate();
  const role = useRole();
  const username = useUsername();
  const isSuperAdmin = isSuperAdminRole(role);
  const { listCohorts, getMyCohorts, listDevices, releaseCohort } = useCohortApi();
  const [cohorts, setCohorts] = useState<CohortSummary[]>([]);
  const [myCohorts, setMyCohorts] = useState<CohortSummary[]>([]);
  const [devices, setDevices] = useState<CohortDevice[]>([]);
  const [cohortsTotalCount, setCohortsTotalCount] = useState(0);
  const [myCohortsTotalCount, setMyCohortsTotalCount] = useState(0);
  const [devicesTotalCount, setDevicesTotalCount] = useState(0);
  const [availableDevices, setAvailableDevices] = useState(0);
  const [reservedDevices, setReservedDevices] = useState(0);
  const [cohortsPageToken, setCohortsPageToken] = useState("");
  const [cohortsNextPageToken, setCohortsNextPageToken] = useState("");
  const [cohortsPageHistory, setCohortsPageHistory] = useState<string[]>([]);
  const [myCohortsPageToken, setMyCohortsPageToken] = useState("");
  const [myCohortsNextPageToken, setMyCohortsNextPageToken] = useState("");
  const [myCohortsPageHistory, setMyCohortsPageHistory] = useState<string[]>([]);
  const [devicesPageToken, setDevicesPageToken] = useState("");
  const [devicesNextPageToken, setDevicesNextPageToken] = useState("");
  const [devicesPageHistory, setDevicesPageHistory] = useState<string[]>([]);
  const [cohortsSearch, setCohortsSearch] = useState("");
  const [myCohortsSearch, setMyCohortsSearch] = useState("");
  const [debouncedCohortsSearch, setDebouncedCohortsSearch] = useState("");
  const [debouncedMyCohortsSearch, setDebouncedMyCohortsSearch] = useState("");
  const [isInitialLoading, setIsInitialLoading] = useState(true);
  const [isMutating, setIsMutating] = useState(false);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [rigAssignmentSearch, setRigAssignmentSearch] = useState("");
  const [debouncedRigAssignmentSearch, setDebouncedRigAssignmentSearch] = useState("");
  const [rigAssignmentFilters, setRigAssignmentFilters] =
    useState<Record<RigAssignmentFilterKey, string[]>>(emptyRigAssignmentFilters);
  const hasLoadedRef = useRef(false);
  const refreshRequestIdRef = useRef(0);

  const rigAssignmentDeviceFilter = useMemo(() => {
    const assignments: CohortDeviceAssignment[] = [];
    rigAssignmentFilters.assignment.forEach((value) => {
      if (value === availableAssignmentValue) assignments.push(CohortDeviceAssignment.AVAILABLE);
      if (value === reservedAssignmentValue) assignments.push(CohortDeviceAssignment.RESERVED);
    });
    const cohortIds: bigint[] = [];
    rigAssignmentFilters.cohort.forEach((value) => {
      if (value === defaultCohortFilterValue) {
        if (!assignments.includes(CohortDeviceAssignment.AVAILABLE)) {
          assignments.push(CohortDeviceAssignment.AVAILABLE);
        }
        return;
      }
      const id = bigintFromFilterValue(value);
      if (id !== undefined) cohortIds.push(id);
    });
    const ownerUserIds: bigint[] = [];
    let includeUnowned = false;
    rigAssignmentFilters.owner.forEach((value) => {
      if (value === unownedFilterValue) {
        includeUnowned = true;
        return;
      }
      const id = bigintFromFilterValue(value);
      if (id !== undefined) ownerUserIds.push(id);
    });
    const siteIds: bigint[] = [];
    let includeUnassignedSite = false;
    rigAssignmentFilters.site.forEach((value) => {
      if (value === unassignedSiteFilterValue) {
        includeUnassignedSite = true;
        return;
      }
      const id = bigintFromFilterValue(value);
      if (id !== undefined) siteIds.push(id);
    });
    const manufacturers: string[] = [];
    const models: string[] = [];
    rigAssignmentFilters.target.forEach((value) => {
      const [manufacturer, model] = value.split(targetFilterSeparator);
      if (manufacturer && model) {
        manufacturers.push(manufacturer);
        models.push(model);
      }
    });
    return {
      assignments,
      cohortIds,
      ownerUserIds,
      includeUnowned,
      manufacturers,
      models,
      siteIds,
      includeUnassignedSite,
      search: debouncedRigAssignmentSearch,
    };
  }, [debouncedRigAssignmentSearch, rigAssignmentFilters]);

  const refresh = useCallback(async () => {
    const requestId = refreshRequestIdRef.current + 1;
    refreshRequestIdRef.current = requestId;
    if (!hasLoadedRef.current) {
      setIsInitialLoading(true);
    }
    setError(null);
    try {
      const [nextCohorts, nextMyCohorts, nextDevices] = await Promise.allSettled([
        listCohorts({
          includeReleased: false,
          pageSize: cohortPageSize,
          pageToken: cohortsPageToken,
          search: debouncedCohortsSearch,
        }),
        getMyCohorts({
          includeReleased: false,
          pageSize: cohortPageSize,
          pageToken: myCohortsPageToken,
          search: debouncedMyCohortsSearch,
        }),
        listDevices({
          pageSize: cohortPageSize,
          pageToken: devicesPageToken,
          filter: rigAssignmentDeviceFilter,
        }),
      ]);
      if (requestId !== refreshRequestIdRef.current) return;
      if (nextCohorts.status === "fulfilled") {
        setCohorts(nextCohorts.value.cohorts);
        setCohortsNextPageToken(nextCohorts.value.nextPageToken);
        setCohortsTotalCount(nextCohorts.value.totalCount);
      }
      if (nextMyCohorts.status === "fulfilled") {
        setMyCohorts(nextMyCohorts.value.cohorts);
        setMyCohortsNextPageToken(nextMyCohorts.value.nextPageToken);
        setMyCohortsTotalCount(nextMyCohorts.value.totalCount);
      }
      if (nextDevices.status === "fulfilled") {
        setDevices(nextDevices.value.devices);
        setDevicesNextPageToken(nextDevices.value.nextPageToken);
        setDevicesTotalCount(nextDevices.value.totalCount);
        setAvailableDevices(nextDevices.value.availableCount);
        setReservedDevices(nextDevices.value.reservedCount);
      }
      if (
        nextCohorts.status === "rejected" ||
        nextMyCohorts.status === "rejected" ||
        nextDevices.status === "rejected"
      ) {
        setError("Couldn't load cohorts");
      }
      hasLoadedRef.current = true;
    } catch {
      if (requestId !== refreshRequestIdRef.current) return;
      setError("Couldn't load cohorts");
    } finally {
      if (requestId === refreshRequestIdRef.current) {
        hasLoadedRef.current = true;
        setIsInitialLoading(false);
      }
    }
  }, [
    debouncedCohortsSearch,
    debouncedMyCohortsSearch,
    cohortsPageToken,
    devicesPageToken,
    getMyCohorts,
    listCohorts,
    listDevices,
    myCohortsPageToken,
    rigAssignmentDeviceFilter,
  ]);

  useEffect(() => {
    const timeoutId = window.setTimeout(() => setDebouncedCohortsSearch(cohortsSearch.trim()), 250);
    return () => window.clearTimeout(timeoutId);
  }, [cohortsSearch]);

  useEffect(() => {
    const timeoutId = window.setTimeout(() => setDebouncedMyCohortsSearch(myCohortsSearch.trim()), 250);
    return () => window.clearTimeout(timeoutId);
  }, [myCohortsSearch]);

  useEffect(() => {
    const timeoutId = window.setTimeout(() => setDebouncedRigAssignmentSearch(rigAssignmentSearch.trim()), 250);
    return () => window.clearTimeout(timeoutId);
  }, [rigAssignmentSearch]);

  useEffect(() => {
    queueMicrotask(() => void refresh());
  }, [refresh]);

  const rigAssignmentFilterOptions = useMemo<FilterChipsBarFilter[]>(
    () => [
      {
        key: "assignment",
        title: "Assignment",
        options: [
          { id: availableAssignmentValue, label: "Available" },
          { id: reservedAssignmentValue, label: "Reserved" },
        ],
        selectedValues: rigAssignmentFilters.assignment,
      },
      {
        key: "cohort",
        title: "Cohort",
        options: uniqueOptions(
          devices.map((device) => ({
            id: getCohortFilterValue(device),
            label:
              device.effectiveCohort && !device.effectiveCohort.isDefault ? device.effectiveCohort.label : "Default",
          })),
        ),
        selectedValues: rigAssignmentFilters.cohort,
      },
      {
        key: "owner",
        title: "Owner",
        options: uniqueOptions(
          devices.map((device) => ({
            id: getOwnerFilterValue(device),
            label: getOwnerLabel(device),
          })),
        ),
        selectedValues: rigAssignmentFilters.owner,
      },
      {
        key: "target",
        title: "Product",
        options: uniqueOptions(
          devices
            .map((device) => ({
              id: getTargetFilterValue(device),
              label: getTargetLabel(device),
            }))
            .filter((option) => option.id !== ""),
        ),
        selectedValues: rigAssignmentFilters.target,
      },
      {
        key: "site",
        title: "Site",
        options: uniqueOptions(
          devices.map((device) => ({
            id: getSiteFilterValue(device),
            label: getSiteLabel(device),
          })),
        ),
        selectedValues: rigAssignmentFilters.site,
      },
    ],
    [devices, rigAssignmentFilters],
  );

  const hasRigAssignmentFilters =
    rigAssignmentSearch.trim() !== "" || Object.values(rigAssignmentFilters).some((values) => values.length > 0);

  const visibleReservedDevices = useMemo(
    () => devices.filter((device) => device.effectiveCohort && !device.effectiveCohort.isDefault).length,
    [devices],
  );
  const visibleAvailableDevices = devices.length - visibleReservedDevices;
  const shouldUseVisibleAssignmentCounts = devices.length > 0 && availableDevices === 0 && reservedDevices === 0;
  const displayedAvailableDevices = shouldUseVisibleAssignmentCounts ? visibleAvailableDevices : availableDevices;
  const displayedReservedDevices = shouldUseVisibleAssignmentCounts ? visibleReservedDevices : reservedDevices;
  const cohortsPageIndex = cohortsPageHistory.length;
  const myCohortsPageIndex = myCohortsPageHistory.length;
  const devicesPageIndex = devicesPageHistory.length;
  const displayedCohortsTotalCount = displayedTotalCount(cohortsTotalCount, cohortsPageIndex, cohorts.length);
  const displayedMyCohortsTotalCount = displayedTotalCount(myCohortsTotalCount, myCohortsPageIndex, myCohorts.length);
  const displayedDevicesTotalCount = displayedTotalCount(devicesTotalCount, devicesPageIndex, devices.length);

  const detailHref = useCallback(
    (cohortId: bigint) => scopedPath(`/cohorts/${cohortId.toString()}`, activeSite),
    [activeSite],
  );

  const handleRelease = useCallback(
    async (cohort: CohortSummary) => {
      setIsMutating(true);
      setError(null);
      try {
        await releaseCohort({ cohortId: cohort.id });
        pushToast({ message: `Cohort "${cohort.label}" released`, status: STATUSES.success });
        await refresh();
      } catch {
        setError("Couldn't release cohort");
      } finally {
        setIsMutating(false);
      }
    },
    [refresh, releaseCohort],
  );

  const handleRigAssignmentFilterChange = useCallback((key: string, selectedValues: string[]) => {
    setRigAssignmentFilters((prev) => ({ ...prev, [key]: selectedValues }));
    setDevicesPageToken("");
    setDevicesPageHistory([]);
  }, []);

  const clearRigAssignmentFilters = useCallback(() => {
    setRigAssignmentSearch("");
    setRigAssignmentFilters(emptyRigAssignmentFilters());
    setDevicesPageToken("");
    setDevicesPageHistory([]);
  }, []);

  const handleRigAssignmentSearchChange = useCallback((value: string) => {
    setRigAssignmentSearch(value);
    setDevicesPageToken("");
    setDevicesPageHistory([]);
  }, []);

  const handleCohortsSearchChange = useCallback((value: string) => {
    setCohortsSearch(value);
    setCohortsPageToken("");
    setCohortsPageHistory([]);
  }, []);

  const handleMyCohortsSearchChange = useCallback((value: string) => {
    setMyCohortsSearch(value);
    setMyCohortsPageToken("");
    setMyCohortsPageHistory([]);
  }, []);

  const goToNextCohortsPage = useCallback(() => {
    if (!cohortsNextPageToken) return;
    setCohortsPageHistory((prev) => [...prev, cohortsPageToken]);
    setCohortsPageToken(cohortsNextPageToken);
  }, [cohortsNextPageToken, cohortsPageToken]);

  const goToPreviousCohortsPage = useCallback(() => {
    setCohortsPageHistory((prev) => {
      if (prev.length === 0) return prev;
      const next = prev.slice(0, -1);
      setCohortsPageToken(prev[prev.length - 1] ?? "");
      return next;
    });
  }, []);

  const goToNextMyCohortsPage = useCallback(() => {
    if (!myCohortsNextPageToken) return;
    setMyCohortsPageHistory((prev) => [...prev, myCohortsPageToken]);
    setMyCohortsPageToken(myCohortsNextPageToken);
  }, [myCohortsNextPageToken, myCohortsPageToken]);

  const goToPreviousMyCohortsPage = useCallback(() => {
    setMyCohortsPageHistory((prev) => {
      if (prev.length === 0) return prev;
      const next = prev.slice(0, -1);
      setMyCohortsPageToken(prev[prev.length - 1] ?? "");
      return next;
    });
  }, []);

  const goToNextDevicesPage = useCallback(() => {
    if (!devicesNextPageToken) return;
    setDevicesPageHistory((prev) => [...prev, devicesPageToken]);
    setDevicesPageToken(devicesNextPageToken);
  }, [devicesNextPageToken, devicesPageToken]);

  const goToPreviousDevicesPage = useCallback(() => {
    setDevicesPageHistory((prev) => {
      if (prev.length === 0) return prev;
      const next = prev.slice(0, -1);
      setDevicesPageToken(prev[prev.length - 1] ?? "");
      return next;
    });
  }, []);

  if (isInitialLoading) {
    return (
      <div className="flex min-h-96 items-center justify-center">
        <ProgressCircular size={48} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6 p-6 laptop:p-10" data-testid="cohorts-page">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <Header title="Cohorts" titleSize="text-heading-300" />
          <div className="mt-2 flex flex-wrap gap-x-6 gap-y-2 text-300 text-text-primary-70">
            <span>{pluralize(displayedAvailableDevices, "available miner")}</span>
            <span>{pluralize(displayedReservedDevices, "reserved miner")}</span>
            <span>{pluralize(displayedCohortsTotalCount, "active cohort")}</span>
          </div>
        </div>
        <Button
          text="Create cohort"
          size={sizes.compact}
          variant={variants.primary}
          prefixIcon={<Plus />}
          onClick={() => setShowCreateModal(true)}
        />
      </div>

      {error ? <Callout intent="danger" prefixIcon={<Alert />} title={error} /> : null}

      <section className="grid items-start gap-6 desktop:grid-cols-[1fr_1fr]">
        <CohortList
          title="Active cohorts"
          cohorts={cohorts}
          totalCount={displayedCohortsTotalCount}
          pageIndex={cohortsPageIndex}
          search={cohortsSearch}
          emptyText={cohortsSearch.trim() ? "No active cohorts match this search." : "No active cohorts."}
          isMutating={isMutating}
          canGoPrevious={cohortsPageHistory.length > 0}
          canGoNext={cohortsNextPageToken !== ""}
          detailHref={detailHref}
          onSearchChange={handleCohortsSearchChange}
          onPreviousPage={goToPreviousCohortsPage}
          onNextPage={goToNextCohortsPage}
          canRelease={(cohort) => isSuperAdmin || isOwnedByCurrentUser(cohort, username)}
          onRelease={handleRelease}
          onView={(cohort) => navigate(detailHref(cohort.id))}
        />
        <CohortList
          title="My cohorts"
          cohorts={myCohorts}
          totalCount={displayedMyCohortsTotalCount}
          pageIndex={myCohortsPageIndex}
          search={myCohortsSearch}
          emptyText={myCohortsSearch.trim() ? "No owned cohorts match this search." : "No owned active cohorts."}
          isMutating={isMutating}
          canGoPrevious={myCohortsPageHistory.length > 0}
          canGoNext={myCohortsNextPageToken !== ""}
          detailHref={detailHref}
          onSearchChange={handleMyCohortsSearchChange}
          onPreviousPage={goToPreviousMyCohortsPage}
          onNextPage={goToNextMyCohortsPage}
          canRelease={(cohort) => isSuperAdmin || isOwnedByCurrentUser(cohort, username)}
          onRelease={handleRelease}
          onView={(cohort) => navigate(detailHref(cohort.id))}
        />
      </section>

      <section className="overflow-hidden rounded-lg border border-border-5">
        <div className="border-b border-border-5 px-4 py-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <Header title="Miner assignments" titleSize="text-heading-100" />
            <span className="text-200 text-text-primary-70">{pluralize(displayedDevicesTotalCount, "miner")}</span>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Search compact initValue={rigAssignmentSearch} onChange={handleRigAssignmentSearchChange} />
            <FilterChipsBar
              filters={rigAssignmentFilterOptions}
              onChange={handleRigAssignmentFilterChange}
              onClearAll={clearRigAssignmentFilters}
              triggerTestId="cohort-rig-assignment-add-filter"
            />
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full table-fixed text-left text-300">
            <thead className="bg-surface-raised text-text-primary-70">
              <tr>
                <th className="w-[34%] px-4 py-3 font-medium">Miner</th>
                <th className="w-[28%] px-4 py-3 font-medium">Cohort</th>
                <th className="w-[24%] px-4 py-3 font-medium">Owner</th>
                <th className="w-[14%] px-4 py-3 font-medium">State</th>
              </tr>
            </thead>
            <tbody>
              {devices.map((device) => {
                const cohort = device.effectiveCohort;
                const rigName = cohortDeviceDisplayName(device);
                const rigSecondary = cohortDeviceSecondaryText(device.display, rigName) || device.deviceIdentifier;
                return (
                  <tr key={device.deviceIdentifier} className="border-t border-border-5">
                    <td className="px-4 py-3">
                      <div className="truncate font-medium" title={device.deviceIdentifier}>
                        {rigName}
                      </div>
                      <div className="truncate text-200 text-text-primary-70">{rigSecondary}</div>
                    </td>
                    <td className="truncate px-4 py-3">
                      {cohort && !cohort.isDefault ? (
                        <Link className="hover:underline" to={detailHref(cohort.id)}>
                          {cohort.label}
                        </Link>
                      ) : (
                        "Default"
                      )}
                    </td>
                    <td className="truncate px-4 py-3">{cohort?.ownerUsername || "Unowned"}</td>
                    <td className="px-4 py-3">{cohortStateLabel(cohort?.state)}</td>
                  </tr>
                );
              })}
              {devices.length === 0 ? (
                <tr>
                  <td className="px-4 py-8 text-text-primary-70" colSpan={4}>
                    {hasRigAssignmentFilters ? "No miners match these filters." : "No miners found."}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <PaginationControls
          itemCount={devices.length}
          totalCount={displayedDevicesTotalCount}
          pageIndex={devicesPageIndex}
          itemName={{ singular: "miner", plural: "miners" }}
          canGoPrevious={devicesPageHistory.length > 0}
          canGoNext={devicesNextPageToken !== ""}
          onPrevious={goToPreviousDevicesPage}
          onNext={goToNextDevicesPage}
        />
      </section>

      <CohortModal show={showCreateModal} onDismiss={() => setShowCreateModal(false)} onSuccess={refresh} />
    </div>
  );
};

interface CohortListProps {
  title: string;
  cohorts: CohortSummary[];
  totalCount: number;
  pageIndex: number;
  search: string;
  emptyText: string;
  isMutating: boolean;
  canGoPrevious: boolean;
  canGoNext: boolean;
  detailHref: (cohortId: bigint) => string;
  onSearchChange: (value: string) => void;
  onPreviousPage: () => void;
  onNextPage: () => void;
  canRelease: (cohort: CohortSummary) => boolean;
  onRelease: (cohort: CohortSummary) => void;
  onView: (cohort: CohortSummary) => void;
}

const CohortList = ({
  title,
  cohorts,
  totalCount,
  pageIndex,
  search,
  emptyText,
  isMutating,
  canGoPrevious,
  canGoNext,
  detailHref,
  onSearchChange,
  onPreviousPage,
  onNextPage,
  canRelease,
  onRelease,
  onView,
}: CohortListProps) => (
  <section className="overflow-hidden rounded-lg border border-border-5">
    <div className="border-b border-border-5 px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Header title={title} titleSize="text-heading-100" />
        <span className="text-200 text-text-primary-70">{pluralize(totalCount, "cohort")}</span>
      </div>
      <div className="mt-3">
        <Search compact initValue={search} onChange={onSearchChange} />
      </div>
    </div>
    <div className="divide-y divide-border-5">
      {cohorts.map((cohort) => (
        <div key={cohort.id.toString()} className="grid gap-3 px-4 py-3 desktop:grid-cols-[minmax(0,1fr)_auto]">
          <Link to={detailHref(cohort.id)} className="min-w-0">
            <div className="truncate font-medium text-text-primary hover:underline">{cohort.label}</div>
            <div className="truncate text-200 text-text-primary-70">
              {cohort.explicitMemberCount.toString()} miners · {cohort.ownerUsername || "Unowned"} ·{" "}
              {formatCohortTimestamp(cohort.expiresAt)}
            </div>
          </Link>
          <div className="flex items-center gap-2">
            {isActiveNonDefaultCohort(cohort) && canRelease(cohort) ? (
              <Button
                text="Release"
                size={sizes.compact}
                variant={variants.secondary}
                disabled={isMutating}
                onClick={() => onRelease(cohort)}
              />
            ) : null}
            <Button
              size={sizes.compact}
              variant={variants.secondary}
              ariaLabel={`View ${cohort.label}`}
              prefixIcon={<ArrowRight />}
              onClick={() => onView(cohort)}
            />
          </div>
        </div>
      ))}
      {cohorts.length === 0 ? <div className="px-4 py-8 text-300 text-text-primary-70">{emptyText}</div> : null}
    </div>
    <PaginationControls
      itemCount={cohorts.length}
      totalCount={totalCount}
      pageIndex={pageIndex}
      itemName={{ singular: "cohort", plural: "cohorts" }}
      canGoPrevious={canGoPrevious}
      canGoNext={canGoNext}
      onPrevious={onPreviousPage}
      onNext={onNextPage}
    />
  </section>
);

interface PaginationControlsProps {
  itemCount: number;
  totalCount: number;
  pageIndex: number;
  itemName: {
    singular: string;
    plural: string;
  };
  canGoPrevious: boolean;
  canGoNext: boolean;
  onPrevious: () => void;
  onNext: () => void;
}

const PaginationControls = ({
  itemCount,
  totalCount,
  pageIndex,
  itemName,
  canGoPrevious,
  canGoNext,
  onPrevious,
  onNext,
}: PaginationControlsProps) => {
  if (itemCount === 0 && totalCount === 0 && !canGoPrevious && !canGoNext) return null;

  const firstItemIndex = pageIndex * cohortPageSize + 1;
  const lastItemIndex = pageEndCount(pageIndex, itemCount);

  return (
    <div className="flex flex-col items-center gap-4 border-t border-border-5 px-4 py-6">
      <span className="text-300 text-text-primary">
        Showing {firstItemIndex.toString()}-{lastItemIndex.toString()} of {totalCount.toString()}{" "}
        {totalCount === 1 ? itemName.singular : itemName.plural}
      </span>
      <div className="flex gap-3">
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Previous page"
          prefixIcon={<ChevronDown className="rotate-90" />}
          onClick={onPrevious}
          disabled={!canGoPrevious}
        />
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Next page"
          prefixIcon={<ChevronDown className="rotate-270" />}
          onClick={onNext}
          disabled={!canGoNext}
        />
      </div>
    </div>
  );
};

export default CohortsPage;
