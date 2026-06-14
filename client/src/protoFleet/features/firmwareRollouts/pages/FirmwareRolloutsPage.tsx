import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { DeviceSelector } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  DeviceIdentifierListSchema,
  DeviceSelectorSchema,
} from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  type FirmwareRollout,
  type FirmwareRolloutEvent,
  FirmwareRolloutState,
  type FirmwareRolloutTarget,
  FirmwareRolloutTargetState,
} from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { useFirmwareRolloutApi } from "@/protoFleet/api/useFirmwareRolloutApi";
import useFleet from "@/protoFleet/api/useFleet";
import {
  FileDropZone,
  FileErrorStatus,
  FileProcessingStatus,
  FileReadyStatus,
  useFirmwareUpload,
} from "@/protoFleet/components/FirmwareUpload";
import Button, { sizes, variants } from "@/shared/components/Button";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

const rolloutsPageSize = 50;
const targetsPageSize = 100;
const pollingMs = 5000;
const pairedOnly = [PairingStatus.PAIRED];

function timestampLabel(seconds?: bigint): string {
  if (!seconds) return "—";
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(Number(seconds) * 1000));
}

function rolloutStateLabel(state: FirmwareRolloutState): string {
  switch (state) {
    case FirmwareRolloutState.DRAFT:
      return "Draft";
    case FirmwareRolloutState.RUNNING:
      return "Running";
    case FirmwareRolloutState.PAUSED:
      return "Paused";
    case FirmwareRolloutState.COMPLETED:
      return "Completed";
    case FirmwareRolloutState.COMPLETED_WITH_FAILURES:
      return "Completed with failures";
    case FirmwareRolloutState.CANCELED:
      return "Canceled";
    default:
      return "Unknown";
  }
}

function targetStateLabel(state: FirmwareRolloutTargetState): string {
  switch (state) {
    case FirmwareRolloutTargetState.PENDING:
      return "Pending";
    case FirmwareRolloutTargetState.DISPATCHING:
      return "Dispatching";
    case FirmwareRolloutTargetState.DISPATCHED:
      return "In progress";
    case FirmwareRolloutTargetState.SUCCEEDED:
      return "Succeeded";
    case FirmwareRolloutTargetState.FAILED:
      return "Failed";
    case FirmwareRolloutTargetState.CANCELED:
      return "Canceled";
    default:
      return "Unknown";
  }
}

function stateClassName(state: FirmwareRolloutState | FirmwareRolloutTargetState): string {
  if (state === FirmwareRolloutState.COMPLETED || state === FirmwareRolloutTargetState.SUCCEEDED) {
    return "bg-intent-success-10 text-intent-success";
  }
  if (
    state === FirmwareRolloutState.COMPLETED_WITH_FAILURES ||
    state === FirmwareRolloutTargetState.FAILED ||
    state === FirmwareRolloutState.CANCELED ||
    state === FirmwareRolloutTargetState.CANCELED
  ) {
    return "bg-intent-critical-10 text-text-critical";
  }
  if (state === FirmwareRolloutState.RUNNING || state === FirmwareRolloutTargetState.DISPATCHED) {
    return "bg-core-accent-10 text-text-primary";
  }
  return "bg-core-primary-5 text-text-primary";
}

function buildDeviceSelector(allDevices: boolean, deviceIds: string[]): DeviceSelector {
  if (allDevices) {
    return create(DeviceSelectorSchema, { selectionType: { case: "allDevices", value: true } });
  }
  return create(DeviceSelectorSchema, {
    selectionType: {
      case: "deviceList",
      value: create(DeviceIdentifierListSchema, { deviceIdentifiers: deviceIds }),
    },
  });
}

function isTerminal(state: FirmwareRolloutState): boolean {
  return (
    state === FirmwareRolloutState.COMPLETED ||
    state === FirmwareRolloutState.COMPLETED_WITH_FAILURES ||
    state === FirmwareRolloutState.CANCELED
  );
}

const FirmwareRolloutsPage = () => {
  const firmwareApi = useFirmwareApi();
  const rolloutApi = useFirmwareRolloutApi();
  const upload = useFirmwareUpload(true);
  const fleet = useFleet({ pageSize: 50, pairingStatuses: pairedOnly });
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [rollouts, setRollouts] = useState<FirmwareRollout[]>([]);
  const [selectedRollout, setSelectedRollout] = useState<FirmwareRollout | null>(null);
  const [targets, setTargets] = useState<FirmwareRolloutTarget[]>([]);
  const [events, setEvents] = useState<FirmwareRolloutEvent[]>([]);
  const [targetPageToken, setTargetPageToken] = useState("");
  const [hasMoreTargets, setHasMoreTargets] = useState(false);
  const [failedOnly, setFailedOnly] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState({
    name: "",
    firmwareFileId: "",
    allDevices: false,
    batchSize: "25",
    batchIntervalSeconds: "300",
  });
  const [selectedMinerIds, setSelectedMinerIds] = useState<string[]>([]);

  const selectedFirmware = firmwareFiles.find((file) => file.id === form.firmwareFileId);
  const effectiveFirmwareFileId = upload.firmwareFileId ?? form.firmwareFileId;
  const effectiveFirmwareSize = upload.file?.size ?? selectedFirmware?.size ?? 0;
  const effectiveFirmwareName = upload.file?.name ?? selectedFirmware?.filename ?? "";
  const batchSize = Number.parseInt(form.batchSize, 10);
  const batchIntervalSeconds = Number.parseInt(form.batchIntervalSeconds, 10);
  const estimatedInFlightBytes =
    effectiveFirmwareSize > 0 && Number.isFinite(batchSize) ? effectiveFirmwareSize * batchSize : 0;

  const loadRollouts = useCallback(async () => {
    const response = await rolloutApi.listRollouts("", rolloutsPageSize);
    setRollouts(response.rollouts);
    if (selectedRollout) {
      const updated = response.rollouts.find((rollout) => rollout.rolloutId === selectedRollout.rolloutId);
      if (updated) setSelectedRollout(updated);
    }
  }, [rolloutApi, selectedRollout]);

  const loadSelectedDetail = useCallback(
    async (rollout: FirmwareRollout, append = false, token = "", failedFilter = failedOnly) => {
      const [detail, targetPage, timeline] = await Promise.all([
        rolloutApi.getRollout(rollout.rolloutId),
        rolloutApi.listTargets({
          rolloutId: rollout.rolloutId,
          pageSize: targetsPageSize,
          pageToken: token,
          stateFilter: failedFilter ? FirmwareRolloutTargetState.FAILED : undefined,
        }),
        rolloutApi.listEvents(rollout.rolloutId),
      ]);
      setSelectedRollout(detail);
      setTargets((prev) => (append ? [...prev, ...targetPage.targets] : targetPage.targets));
      setTargetPageToken(targetPage.nextPageToken);
      setHasMoreTargets(targetPage.nextPageToken !== "");
      setEvents(timeline);
    },
    [failedOnly, rolloutApi],
  );

  useEffect(() => {
    let canceled = false;
    Promise.all([firmwareApi.listFirmwareFiles(), rolloutApi.listRollouts("", rolloutsPageSize)])
      .then(([files, rolloutPage]) => {
        if (canceled) return;
        setFirmwareFiles(files);
        setRollouts(rolloutPage.rollouts);
        setForm((prev) => ({ ...prev, firmwareFileId: prev.firmwareFileId || files[0]?.id || "" }));
      })
      .catch((err) => {
        if (!canceled) setError(err instanceof Error ? err.message : "Failed to load firmware rollouts");
      })
      .finally(() => {
        if (!canceled) setIsLoading(false);
      });
    return () => {
      canceled = true;
    };
  }, [firmwareApi, rolloutApi]);

  useEffect(() => {
    if (!selectedRollout || isTerminal(selectedRollout.state)) return;
    const interval = setInterval(() => {
      void loadSelectedDetail(selectedRollout);
      void loadRollouts();
    }, pollingMs);
    return () => clearInterval(interval);
  }, [loadRollouts, loadSelectedDetail, selectedRollout]);

  const updateForm = (key: keyof typeof form, value: string | boolean) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  const handleFailedOnlyChange = (value: boolean) => {
    setFailedOnly(value);
    if (selectedRollout) {
      void loadSelectedDetail(selectedRollout, false, "", value);
    }
  };

  const toggleMiner = (deviceIdentifier: string) => {
    setSelectedMinerIds((prev) =>
      prev.includes(deviceIdentifier) ? prev.filter((id) => id !== deviceIdentifier) : [...prev, deviceIdentifier],
    );
  };

  const selectCurrentPage = () => {
    setSelectedMinerIds((prev) => {
      const next = new Set(prev);
      fleet.minerIds.forEach((id) => next.add(id));
      return [...next];
    });
  };

  const clearSelectedMiners = () => setSelectedMinerIds([]);

  const canCreate =
    form.name.trim() !== "" &&
    Boolean(effectiveFirmwareFileId) &&
    (form.allDevices || selectedMinerIds.length > 0) &&
    Number.isFinite(batchSize) &&
    batchSize > 0 &&
    Number.isFinite(batchIntervalSeconds) &&
    batchIntervalSeconds >= 0;

  const handleCreate = async () => {
    if (!canCreate) return;
    if (!effectiveFirmwareFileId) return;
    setIsSubmitting(true);
    setError(null);
    try {
      const created = await rolloutApi.createRollout({
        name: form.name.trim(),
        firmwareFileId: effectiveFirmwareFileId,
        deviceSelector: buildDeviceSelector(form.allDevices, selectedMinerIds),
        batchSize,
        batchIntervalSeconds,
      });
      const started = await rolloutApi.startRollout(created.rolloutId);
      await loadRollouts();
      await loadSelectedDetail(started);
      setForm((prev) => ({ ...prev, name: "" }));
      setSelectedMinerIds([]);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create rollout");
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleAction = async (action: "pause" | "resume" | "cancel" | "retry") => {
    if (!selectedRollout) return;
    setError(null);
    try {
      const next =
        action === "pause"
          ? await rolloutApi.pauseRollout(selectedRollout.rolloutId)
          : action === "resume"
            ? await rolloutApi.resumeRollout(selectedRollout.rolloutId)
            : action === "cancel"
              ? await rolloutApi.cancelRollout(selectedRollout.rolloutId)
              : await rolloutApi.retryFailedTargets(selectedRollout.rolloutId);
      await loadRollouts();
      await loadSelectedDetail(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Rollout action failed");
    }
  };

  const loadMoreTargets = () => {
    if (selectedRollout && hasMoreTargets) {
      void loadSelectedDetail(selectedRollout, true, targetPageToken);
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6 p-6 laptop:p-10">
      <div className="flex items-center justify-between">
        <Header title="Firmware Rollouts" titleSize="text-heading-300" />
        <Button variant={variants.secondary} size={sizes.compact} text="Refresh" onClick={() => void loadRollouts()} />
      </div>

      {error ? <div className="rounded-lg bg-intent-critical-10 p-3 text-300 text-text-critical">{error}</div> : null}

      <section className="rounded-xl border border-border-5 bg-surface-base p-5 shadow-50">
        <div className="mb-4">
          <div className="text-heading-100 text-text-primary">Create rollout</div>
          <div className="text-300 text-text-primary-50">
            Rollouts freeze their target set at start and dispatch only one configured batch at a time.
          </div>
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <label className="flex flex-col gap-1 text-300 text-text-primary">
            Rollout name
            <input
              className="rounded-lg border border-border-5 bg-surface-base px-3 py-2"
              value={form.name}
              onChange={(e) => updateForm("name", e.target.value)}
              placeholder="June firmware update"
            />
          </label>
          <label className="flex flex-col gap-1 text-300 text-text-primary">
            Firmware payload
            <select
              className="rounded-lg border border-border-5 bg-surface-base px-3 py-2"
              value={form.firmwareFileId}
              onChange={(e) => {
                upload.reset();
                updateForm("firmwareFileId", e.target.value);
              }}
            >
              {firmwareFiles.length === 0 ? <option value="">No firmware files uploaded</option> : null}
              {firmwareFiles.map((file) => (
                <option key={file.id} value={file.id}>
                  {file.filename} ({formatFileSize(file.size)})
                </option>
              ))}
            </select>
          </label>
          <div className="flex flex-col gap-3 rounded-lg border border-border-5 p-3 laptop:col-span-2">
            <div>
              <div className="text-300 font-semibold text-text-primary">Upload firmware from this rollout</div>
              <div className="text-200 text-text-primary-50">
                Uploading here uses the same firmware storage and makes the payload available immediately.
              </div>
            </div>
            {upload.state === "idle" && upload.serverConfig ? (
              <FileDropZone extensions={upload.serverConfig.allowedExtensions} onFileSelect={upload.processFile} />
            ) : null}
            {(upload.state === "hashing" || upload.state === "checking" || upload.state === "uploading") &&
            upload.file ? (
              <FileProcessingStatus
                state={upload.state}
                fileName={upload.file.name}
                fileSize={upload.file.size}
                uploadProgress={upload.uploadProgress}
              />
            ) : null}
            {upload.state === "ready" && upload.file ? (
              <FileReadyStatus fileName={upload.file.name} fileSize={upload.file.size} />
            ) : null}
            {upload.state === "error" && upload.errorMessage ? (
              <FileErrorStatus message={upload.errorMessage} onRetry={upload.retry} />
            ) : null}
            {effectiveFirmwareName ? (
              <div className="text-200 text-text-primary-50">
                Selected payload: {effectiveFirmwareName}
                {effectiveFirmwareSize ? ` (${formatFileSize(effectiveFirmwareSize)})` : ""}
              </div>
            ) : null}
          </div>
          <label className="flex items-center gap-2 text-300 text-text-primary">
            <input
              type="checkbox"
              checked={form.allDevices}
              onChange={(e) => updateForm("allDevices", e.target.checked)}
            />
            Target all paired miners
          </label>
          <div className="text-300 text-text-primary-50">
            Target preview: {form.allDevices ? "All paired miners" : `${selectedMinerIds.length} selected miner(s)`}
          </div>
          {!form.allDevices ? (
            <div className="flex flex-col gap-3 rounded-lg border border-border-5 p-3 laptop:col-span-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <div className="text-300 font-semibold text-text-primary">Select target miners</div>
                  <div className="text-200 text-text-primary-50">
                    Showing {fleet.minerIds.length} of {fleet.totalMiners} paired miners on this page.
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    text="Select page"
                    onClick={selectCurrentPage}
                  />
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    text="Clear"
                    onClick={clearSelectedMiners}
                    disabled={selectedMinerIds.length === 0}
                  />
                </div>
              </div>
              {fleet.isLoading && !fleet.hasInitialLoadCompleted ? (
                <div className="flex justify-center py-4">
                  <ProgressCircular indeterminate />
                </div>
              ) : (
                <div className="max-h-64 overflow-y-auto rounded-lg border border-border-5">
                  <table className="w-full text-200">
                    <thead className="sticky top-0 bg-surface-5 text-left text-text-primary-50">
                      <tr>
                        <th className="px-3 py-2">Select</th>
                        <th className="px-3 py-2">Miner</th>
                        <th className="px-3 py-2">Model</th>
                        <th className="px-3 py-2">IP</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border-5">
                      {fleet.minerIds.map((id) => {
                        const miner = fleet.miners[id];
                        const checked = selectedMinerIds.includes(id);
                        return (
                          <tr key={id} className="hover:bg-surface-5">
                            <td className="px-3 py-2">
                              <input type="checkbox" checked={checked} onChange={() => toggleMiner(id)} />
                            </td>
                            <td className="px-3 py-2">
                              <div className="text-text-primary">{miner?.name || id}</div>
                              <div className="text-100 font-mono text-text-primary-50">{id}</div>
                            </td>
                            <td className="px-3 py-2 text-text-primary-50">
                              {[miner?.manufacturer, miner?.model].filter(Boolean).join(" ") || "—"}
                            </td>
                            <td className="px-3 py-2 text-text-primary-50">{miner?.ipAddress || "—"}</td>
                          </tr>
                        );
                      })}
                      {fleet.minerIds.length === 0 ? (
                        <tr>
                          <td className="px-3 py-4 text-text-primary-50" colSpan={4}>
                            No paired miners found.
                          </td>
                        </tr>
                      ) : null}
                    </tbody>
                  </table>
                </div>
              )}
              <div className="flex items-center justify-between">
                <div className="text-200 text-text-primary-50">{selectedMinerIds.length} miner(s) selected</div>
                <div className="flex gap-2">
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    text="Previous"
                    onClick={fleet.goToPrevPage}
                    disabled={!fleet.hasPreviousPage || fleet.isLoading}
                  />
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    text="Next"
                    onClick={fleet.goToNextPage}
                    disabled={!fleet.hasMore || fleet.isLoading}
                  />
                </div>
              </div>
            </div>
          ) : null}
          <label className="flex flex-col gap-1 text-300 text-text-primary">
            Batch size
            <input
              className="rounded-lg border border-border-5 bg-surface-base px-3 py-2"
              type="number"
              min={1}
              value={form.batchSize}
              onChange={(e) => updateForm("batchSize", e.target.value)}
            />
          </label>
          <label className="flex flex-col gap-1 text-300 text-text-primary">
            Delay between batches (seconds)
            <input
              className="rounded-lg border border-border-5 bg-surface-base px-3 py-2"
              type="number"
              min={0}
              value={form.batchIntervalSeconds}
              onChange={(e) => updateForm("batchIntervalSeconds", e.target.value)}
            />
          </label>
        </div>
        <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
          <div className="text-300 text-text-primary-50">
            Estimated firmware bytes per batch: {estimatedInFlightBytes ? formatFileSize(estimatedInFlightBytes) : "—"}
          </div>
          <Button
            variant={variants.primary}
            text="Create and start rollout"
            disabled={!canCreate || isSubmitting}
            loading={isSubmitting}
            onClick={handleCreate}
          />
        </div>
      </section>

      <div className="grid gap-6 desktop:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
        <section className="rounded-xl border border-border-5 bg-surface-base">
          <div className="border-b border-border-5 p-4">
            <div className="text-heading-100 text-text-primary">Rollout history</div>
          </div>
          <div className="divide-y divide-border-5">
            {rollouts.length === 0 ? (
              <div className="p-6 text-300 text-text-primary-50">No firmware rollouts yet.</div>
            ) : (
              rollouts.map((rollout) => (
                <button
                  key={rollout.rolloutId}
                  type="button"
                  className="w-full cursor-pointer p-4 text-left hover:bg-surface-5"
                  onClick={() => void loadSelectedDetail(rollout)}
                >
                  <div className="flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <div className="truncate text-300 font-semibold text-text-primary">{rollout.name}</div>
                      <div className="text-200 text-text-primary-50">
                        {rollout.targetCount} target(s) · batch {rollout.batchSize} every {rollout.batchIntervalSeconds}
                        s
                      </div>
                    </div>
                    <span className={`text-100 rounded-full px-2 py-1 ${stateClassName(rollout.state)}`}>
                      {rolloutStateLabel(rollout.state)}
                    </span>
                  </div>
                  <div className="mt-2 text-200 text-text-primary-50">
                    Created {timestampLabel(rollout.createdAt?.seconds)}
                  </div>
                </button>
              ))
            )}
          </div>
        </section>

        <section className="rounded-xl border border-border-5 bg-surface-base">
          {!selectedRollout ? (
            <div className="p-6 text-300 text-text-primary-50">
              Select a rollout to inspect progress and retry failures.
            </div>
          ) : (
            <div className="flex flex-col">
              <div className="border-b border-border-5 p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <div className="text-heading-100 text-text-primary">{selectedRollout.name}</div>
                    <div className="text-200 text-text-primary-50">
                      {selectedRollout.rolloutId} · firmware {selectedRollout.firmwareFileId}
                    </div>
                  </div>
                  <span className={`text-100 rounded-full px-2 py-1 ${stateClassName(selectedRollout.state)}`}>
                    {rolloutStateLabel(selectedRollout.state)}
                  </span>
                </div>
                <div className="mt-4 flex flex-wrap gap-2">
                  {selectedRollout.state === FirmwareRolloutState.RUNNING ? (
                    <Button
                      variant={variants.secondary}
                      size={sizes.compact}
                      text="Pause"
                      onClick={() => void handleAction("pause")}
                    />
                  ) : null}
                  {selectedRollout.state === FirmwareRolloutState.PAUSED ? (
                    <Button
                      variant={variants.secondary}
                      size={sizes.compact}
                      text="Resume"
                      onClick={() => void handleAction("resume")}
                    />
                  ) : null}
                  {selectedRollout.state === FirmwareRolloutState.RUNNING ||
                  selectedRollout.state === FirmwareRolloutState.PAUSED ||
                  selectedRollout.state === FirmwareRolloutState.DRAFT ? (
                    <Button
                      variant={variants.secondaryDanger}
                      size={sizes.compact}
                      text="Cancel"
                      onClick={() => void handleAction("cancel")}
                    />
                  ) : null}
                  {selectedRollout.state === FirmwareRolloutState.COMPLETED_WITH_FAILURES ||
                  selectedRollout.state === FirmwareRolloutState.PAUSED ? (
                    <Button
                      variant={variants.primary}
                      size={sizes.compact}
                      text="Retry failed miners"
                      onClick={() => void handleAction("retry")}
                    />
                  ) : null}
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3 p-4 laptop:grid-cols-4">
                {[
                  ["Targeted", selectedRollout.counts?.totalCount ?? selectedRollout.targetCount],
                  ["Pending", selectedRollout.counts?.pendingCount ?? 0],
                  ["In progress", selectedRollout.counts?.inProgressCount ?? 0],
                  ["Succeeded", selectedRollout.counts?.successCount ?? 0],
                  ["Failed", selectedRollout.counts?.failureCount ?? 0],
                  ["Canceled", selectedRollout.counts?.canceledCount ?? 0],
                  ["Retried", selectedRollout.counts?.retriedCount ?? 0],
                ].map(([label, value]) => (
                  <div key={label} className="rounded-lg bg-surface-5 p-3">
                    <div className="text-100 text-text-primary-50">{label}</div>
                    <div className="text-heading-100 text-text-primary">{value}</div>
                  </div>
                ))}
              </div>

              <div className="border-t border-border-5 p-4">
                <div className="mb-3 flex items-center justify-between">
                  <div className="text-300 font-semibold text-text-primary">Miner results</div>
                  <label className="flex items-center gap-2 text-200 text-text-primary">
                    <input
                      type="checkbox"
                      checked={failedOnly}
                      onChange={(e) => handleFailedOnlyChange(e.target.checked)}
                    />
                    Failed only
                  </label>
                </div>
                <div className="max-h-80 overflow-y-auto rounded-lg border border-border-5">
                  <table className="w-full text-200">
                    <thead className="sticky top-0 bg-surface-5 text-left text-text-primary-50">
                      <tr>
                        <th className="px-3 py-2">Miner</th>
                        <th className="px-3 py-2">Status</th>
                        <th className="px-3 py-2">Attempt</th>
                        <th className="px-3 py-2">Message</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border-5">
                      {targets.length === 0 ? (
                        <tr>
                          <td className="px-3 py-4 text-text-primary-50" colSpan={4}>
                            No miner results yet.
                          </td>
                        </tr>
                      ) : (
                        targets.map((target) => (
                          <tr key={target.deviceIdentifier}>
                            <td className="px-3 py-2">
                              <div className="text-text-primary">{target.deviceName || target.deviceIdentifier}</div>
                              <div className="text-100 font-mono text-text-primary-50">
                                {[target.macAddress, target.ipAddress].filter(Boolean).join(" · ")}
                              </div>
                            </td>
                            <td className="px-3 py-2">
                              <span className={`rounded-full px-2 py-1 ${stateClassName(target.state)}`}>
                                {targetStateLabel(target.state)}
                              </span>
                            </td>
                            <td className="px-3 py-2 text-text-primary">{target.currentAttemptNumber || "—"}</td>
                            <td className="max-w-sm truncate px-3 py-2 text-text-primary-50">
                              {target.lastError || "—"}
                            </td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                </div>
                {hasMoreTargets ? (
                  <div className="mt-3 flex justify-center">
                    <Button
                      variant={variants.secondary}
                      size={sizes.compact}
                      text="Load more miners"
                      onClick={loadMoreTargets}
                    />
                  </div>
                ) : null}
              </div>

              <div className="border-t border-border-5 p-4">
                <div className="mb-3 text-300 font-semibold text-text-primary">Timeline</div>
                <div className="flex flex-col gap-2">
                  {events.length === 0 ? (
                    <div className="text-200 text-text-primary-50">No rollout events yet.</div>
                  ) : (
                    events.map((event) => (
                      <div
                        key={`${event.eventType}-${event.createdAt?.seconds}`}
                        className="rounded-lg bg-surface-5 p-3"
                      >
                        <div className="text-300 text-text-primary">{event.message}</div>
                        <div className="text-200 text-text-primary-50">
                          {event.username || event.actorType} · {timestampLabel(event.createdAt?.seconds)}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  );
};

export default FirmwareRolloutsPage;
