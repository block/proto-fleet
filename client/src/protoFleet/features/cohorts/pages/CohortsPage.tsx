import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";

import {
  CohortConfigDimension,
  type CohortSummary,
  CohortTelemetryComparisonWindow,
  type GetCohortTelemetryComparisonResponse,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import CohortComparisonDashboard from "@/protoFleet/features/cohorts/components/CohortComparisonDashboard";
import CohortModal from "@/protoFleet/features/cohorts/components/CohortModal";
import {
  formatCohortExpiryTimeLeft,
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
import ProgressCircular from "@/shared/components/ProgressCircular";
import Search from "@/shared/components/Search";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

const registerPageSize = 25;

const pluralize = (count: number, singular: string, plural = `${singular}s`) =>
  `${count.toLocaleString()} ${count === 1 ? singular : plural}`;

const isOwnedByCurrentUser = (cohort: CohortSummary, username: string) => {
  const ownerUsername = cohort.ownerUsername.trim().toLowerCase();
  const currentUsername = username.trim().toLowerCase();
  return ownerUsername !== "" && currentUsername !== "" && ownerUsername === currentUsername;
};

const formatFirmwareFileInfo = (firmwareFileId: string, firmwareFilesById: Map<string, FirmwareFileInfo>) => {
  const file = firmwareFilesById.get(firmwareFileId);
  if (!file) return firmwareFileId;
  const version = file.firmware_version?.trim();
  const filename = file.filename?.trim();
  if (version && filename) return `${version} (${filename})`;
  return version || filename || firmwareFileId;
};

const formatCohortFirmwareSummary = (cohort: CohortSummary, firmwareFilesById: Map<string, FirmwareFileInfo>) => {
  const targets = cohort.firmwareTargets.filter((target) => target.firmwareFileId);
  if (targets.length > 0) {
    return targets
      .map((target) => {
        const minerType = [target.manufacturer, target.model].filter(Boolean).join(" ") || "Target";
        return `${minerType}: ${formatFirmwareFileInfo(target.firmwareFileId, firmwareFilesById)}`;
      })
      .join(" · ");
  }
  if (cohort.desiredFirmwareFileId) {
    return formatFirmwareFileInfo(cohort.desiredFirmwareFileId, firmwareFilesById);
  }
  return "Not enforced";
};

const CohortExpiryText = ({ cohort }: { cohort: CohortSummary }) => {
  const timeLeft = isActiveNonDefaultCohort(cohort) ? formatCohortExpiryTimeLeft(cohort.expiresAt) : undefined;
  return (
    <>
      {formatCohortTimestamp(cohort.expiresAt)}
      {timeLeft ? <span className="text-text-primary-50"> · {timeLeft}</span> : null}
    </>
  );
};

const RolloutSummary = ({ cohort }: { cohort: CohortSummary }) => {
  const firmwareTargeted = cohort.firmwareProgress?.targetedCount ?? 0;
  const firmwareComplete = cohort.firmwareProgress?.completeCount ?? 0;
  const pools = cohort.configProgress.find((progress) => progress.dimension === CohortConfigDimension.POOLS);
  const poolsTargeted = pools?.targetedCount ?? 0;
  const poolsComplete = pools?.convergedCount ?? 0;
  const firmwareEnforced =
    Boolean(cohort.desiredFirmwareFileId) || cohort.firmwareTargets.some((target) => Boolean(target.firmwareFileId));
  const poolsEnforced = Boolean(cohort.desiredConfig?.pools);

  return (
    <div className="space-y-1 text-200 text-text-primary-70">
      <div>{firmwareEnforced ? `Firmware ${firmwareComplete}/${firmwareTargeted}` : "Firmware not enforced"}</div>
      <div>{poolsEnforced ? `Pools ${poolsComplete}/${poolsTargeted}` : "Pools not enforced"}</div>
    </div>
  );
};

const CohortsPage = () => {
  const activeSite = useRouteSiteScope() ?? DEFAULT_ACTIVE_SITE;
  const navigate = useNavigate();
  const role = useRole();
  const username = useUsername();
  const isSuperAdmin = isSuperAdminRole(role);
  const { listAllCohorts, getTelemetryComparison, releaseCohort } = useCohortApi();
  const { listFirmwareFiles } = useFirmwareApi();
  const [cohorts, setCohorts] = useState<CohortSummary[]>([]);
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [selectedCohortIds, setSelectedCohortIds] = useState<string[]>([]);
  const [comparisonWindow, setComparisonWindow] = useState(CohortTelemetryComparisonWindow.SIX_HOURS);
  const [comparison, setComparison] = useState<GetCohortTelemetryComparisonResponse | null>(null);
  const [comparisonLoading, setComparisonLoading] = useState(false);
  const [comparisonError, setComparisonError] = useState(false);
  const [refreshVersion, setRefreshVersion] = useState(0);
  const [isInitialLoading, setIsInitialLoading] = useState(true);
  const [isMutating, setIsMutating] = useState(false);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [pageIndex, setPageIndex] = useState(0);
  const refreshRequestIdRef = useRef(0);
  const comparisonRequestIdRef = useRef(0);

  const refresh = useCallback(async () => {
    const requestId = refreshRequestIdRef.current + 1;
    refreshRequestIdRef.current = requestId;
    setError(null);
    try {
      const [nextCohorts, nextFirmwareFiles] = await Promise.allSettled([
        listAllCohorts({ includeReleased: false }),
        listFirmwareFiles(),
      ]);
      if (requestId !== refreshRequestIdRef.current) return;
      if (nextCohorts.status === "rejected") {
        setError("Couldn't load cohorts");
        return;
      }
      const activeCohorts = nextCohorts.value;
      setCohorts(activeCohorts);
      setFirmwareFiles(nextFirmwareFiles.status === "fulfilled" ? nextFirmwareFiles.value : []);
      setSelectedCohortIds((current) => {
        const defaultCohort = activeCohorts.find((cohort) => cohort.isDefault);
        if (!defaultCohort) return [];
        const defaultId = defaultCohort.id.toString();
        const availableIDs = new Set(activeCohorts.map((cohort) => cohort.id.toString()));
        const preserved = current.filter((id) => id !== defaultId && availableIDs.has(id)).slice(0, 4);
        const initial =
          current.length === 0
            ? activeCohorts
                .filter((cohort) => !cohort.isDefault)
                .slice(0, 2)
                .map((cohort) => cohort.id.toString())
            : preserved;
        return [defaultId, ...initial];
      });
      setRefreshVersion((version) => version + 1);
    } catch {
      if (requestId === refreshRequestIdRef.current) setError("Couldn't load cohorts");
    } finally {
      if (requestId === refreshRequestIdRef.current) setIsInitialLoading(false);
    }
  }, [listAllCohorts, listFirmwareFiles]);

  useEffect(() => {
    queueMicrotask(() => void refresh());
    const intervalID = window.setInterval(() => void refresh(), POLL_INTERVAL_MS);
    return () => window.clearInterval(intervalID);
  }, [refresh]);

  // Outcome comparisons scan two historical windows. Refresh them once per
  // minute while keeping the lightweight cohort register on the shared 15s
  // polling cadence.
  const comparisonRefreshTick = Math.floor(Math.max(0, refreshVersion - 1) / 4);

  useEffect(() => {
    if (selectedCohortIds.length === 0) {
      return;
    }
    const requestId = comparisonRequestIdRef.current + 1;
    comparisonRequestIdRef.current = requestId;
    const load = async () => {
      setComparisonLoading(true);
      setComparisonError(false);
      try {
        const response = await getTelemetryComparison({
          cohortIds: selectedCohortIds.map((id) => BigInt(id)),
          comparisonWindow,
        });
        if (requestId === comparisonRequestIdRef.current) setComparison(response);
      } catch {
        if (requestId === comparisonRequestIdRef.current) setComparisonError(true);
      } finally {
        if (requestId === comparisonRequestIdRef.current) setComparisonLoading(false);
      }
    };
    queueMicrotask(() => void load());
  }, [comparisonRefreshTick, comparisonWindow, getTelemetryComparison, selectedCohortIds]);

  const detailHref = useCallback(
    (cohortId: bigint) => scopedPath(`/cohorts/${cohortId.toString()}`, activeSite),
    [activeSite],
  );

  const toggleComparedCohort = useCallback(
    (cohortId: string) => {
      const cohort = cohorts.find((item) => item.id.toString() === cohortId);
      if (!cohort || cohort.isDefault) return;
      setSelectedCohortIds((current) => {
        if (current.includes(cohortId)) return current.filter((id) => id !== cohortId);
        if (current.length >= 5) return current;
        return [...current, cohortId];
      });
    },
    [cohorts],
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

  const normalizedSearch = search.trim().toLocaleLowerCase();
  const filteredCohorts = useMemo(
    () =>
      normalizedSearch === ""
        ? cohorts
        : cohorts.filter((cohort) =>
            [cohort.label, cohort.purpose, cohort.ownerUsername]
              .join(" ")
              .toLocaleLowerCase()
              .includes(normalizedSearch),
          ),
    [cohorts, normalizedSearch],
  );
  const pageCount = Math.max(1, Math.ceil(filteredCohorts.length / registerPageSize));
  const safePageIndex = Math.min(pageIndex, pageCount - 1);
  const visibleCohorts = filteredCohorts.slice(
    safePageIndex * registerPageSize,
    safePageIndex * registerPageSize + registerPageSize,
  );
  const totalMiners = cohorts.reduce((total, cohort) => total + Number(cohort.explicitMemberCount), 0);
  const defaultMiners = Number(cohorts.find((cohort) => cohort.isDefault)?.explicitMemberCount ?? 0n);
  const rolloutCohorts = cohorts.filter((cohort) => !cohort.isDefault).length;
  const firmwareFilesById = useMemo(
    () => new Map(firmwareFiles.map((file) => [file.id, file] as const)),
    [firmwareFiles],
  );

  const handleSearchChange = useCallback((value: string) => {
    setSearch(value);
    setPageIndex(0);
  }, []);

  if (isInitialLoading) {
    return (
      <div className="flex min-h-96 items-center justify-center">
        <ProgressCircular size={48} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-8 p-6 laptop:p-10" data-testid="cohorts-page">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <Header title="Cohorts" titleSize="text-heading-300" />
          <p className="mt-2 max-w-2xl text-300 text-text-primary-70">
            Track fleet rollouts, compare desired-state convergence, and monitor operating outcomes.
          </p>
          <div className="mt-3 flex flex-wrap gap-x-6 gap-y-2 text-300 text-text-primary-70">
            <span>{pluralize(totalMiners, "miner")}</span>
            <span>{pluralize(defaultMiners, "miner")} in default</span>
            <span>{pluralize(rolloutCohorts, "rollout cohort")}</span>
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

      {cohorts.length > 0 ? (
        <CohortComparisonDashboard
          cohorts={cohorts}
          selectedIds={selectedCohortIds}
          comparisonWindow={comparisonWindow}
          comparison={comparison}
          comparisonLoading={comparisonLoading}
          comparisonError={comparisonError}
          onToggleCohort={toggleComparedCohort}
          onWindowChange={setComparisonWindow}
        />
      ) : (
        <section className="rounded-xl border border-border-5 bg-surface-base px-6 py-10 text-center">
          <h2 className="text-heading-200 text-text-primary">No active cohorts are available</h2>
          <p className="mt-2 text-300 text-text-primary-70">Create a cohort to begin a managed rollout.</p>
        </section>
      )}

      <section
        className="overflow-hidden rounded-xl border border-border-5 bg-surface-base"
        data-testid="cohort-register"
      >
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border-5 px-5 py-4">
          <div>
            <Header title="Cohort register" titleSize="text-heading-100" />
            <p className="mt-1 text-200 text-text-primary-70">All active cohorts, including the default cohort.</p>
          </div>
          <div className="w-full tablet:w-72">
            <Search compact initValue={search} onChange={handleSearchChange} />
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[980px] text-left text-300">
            <thead className="bg-surface-raised text-text-primary-70">
              <tr>
                <th className="w-[22%] px-5 py-3 font-medium">Cohort</th>
                <th className="w-[12%] px-5 py-3 font-medium">Allocation</th>
                <th className="w-[28%] px-5 py-3 font-medium">Desired state</th>
                <th className="w-[16%] px-5 py-3 font-medium">Convergence</th>
                <th className="w-[15%] px-5 py-3 font-medium">Owner and expiry</th>
                <th className="w-[7%] px-5 py-3 font-medium" />
              </tr>
            </thead>
            <tbody>
              {visibleCohorts.map((cohort) => (
                <tr key={cohort.id.toString()} className="border-t border-border-5 align-top">
                  <td className="px-5 py-4">
                    <Link className="font-medium text-text-primary hover:underline" to={detailHref(cohort.id)}>
                      {cohort.label}
                    </Link>
                    <div className="mt-1 line-clamp-2 text-200 text-text-primary-70">
                      {cohort.isDefault ? "Default cohort" : cohort.purpose || "No purpose provided"}
                    </div>
                  </td>
                  <td className="px-5 py-4 font-medium text-text-primary">
                    {pluralize(Number(cohort.explicitMemberCount), "miner")}
                  </td>
                  <td className="px-5 py-4">
                    <div className="line-clamp-2 text-200 text-text-primary">
                      Firmware · {formatCohortFirmwareSummary(cohort, firmwareFilesById)}
                    </div>
                    <div className="mt-1 text-200 text-text-primary-70">
                      Pools · {cohort.desiredConfig?.pools ? "Configured" : "Not enforced"}
                    </div>
                  </td>
                  <td className="px-5 py-4">
                    <RolloutSummary cohort={cohort} />
                  </td>
                  <td className="px-5 py-4 text-200 text-text-primary-70">
                    <div className="text-text-primary">{cohort.ownerUsername || "Unowned"}</div>
                    <div className="mt-1">
                      <CohortExpiryText cohort={cohort} />
                    </div>
                  </td>
                  <td className="px-5 py-4">
                    <div className="flex justify-end gap-2">
                      {isActiveNonDefaultCohort(cohort) && (isSuperAdmin || isOwnedByCurrentUser(cohort, username)) ? (
                        <Button
                          text="Release"
                          size={sizes.compact}
                          variant={variants.secondary}
                          disabled={isMutating}
                          onClick={() => void handleRelease(cohort)}
                        />
                      ) : null}
                      <Button
                        size={sizes.compact}
                        variant={variants.secondary}
                        ariaLabel={`View ${cohort.label}`}
                        prefixIcon={<ArrowRight />}
                        onClick={() => navigate(detailHref(cohort.id))}
                      />
                    </div>
                  </td>
                </tr>
              ))}
              {visibleCohorts.length === 0 ? (
                <tr>
                  <td className="px-5 py-10 text-center text-text-primary-70" colSpan={6}>
                    No cohorts match this search.
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <RegisterPagination
          pageIndex={safePageIndex}
          pageCount={pageCount}
          totalCount={filteredCohorts.length}
          visibleCount={visibleCohorts.length}
          onPrevious={() => setPageIndex((current) => Math.max(0, current - 1))}
          onNext={() => setPageIndex((current) => Math.min(pageCount - 1, current + 1))}
        />
      </section>

      <CohortModal show={showCreateModal} onDismiss={() => setShowCreateModal(false)} onSuccess={refresh} />
    </div>
  );
};

const RegisterPagination = ({
  pageIndex,
  pageCount,
  totalCount,
  visibleCount,
  onPrevious,
  onNext,
}: {
  pageIndex: number;
  pageCount: number;
  totalCount: number;
  visibleCount: number;
  onPrevious: () => void;
  onNext: () => void;
}) => {
  if (totalCount <= registerPageSize) return null;
  const first = pageIndex * registerPageSize + 1;
  const last = pageIndex * registerPageSize + visibleCount;
  return (
    <div className="flex flex-col items-center gap-3 border-t border-border-5 px-5 py-5">
      <span className="text-300 text-text-primary">
        Showing {first}-{last} of {totalCount} cohorts
      </span>
      <div className="flex gap-3">
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Previous page"
          prefixIcon={<ChevronDown className="rotate-90" />}
          onClick={onPrevious}
          disabled={pageIndex === 0}
        />
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Next page"
          prefixIcon={<ChevronDown className="rotate-270" />}
          onClick={onNext}
          disabled={pageIndex >= pageCount - 1}
        />
      </div>
    </div>
  );
};

export default CohortsPage;
