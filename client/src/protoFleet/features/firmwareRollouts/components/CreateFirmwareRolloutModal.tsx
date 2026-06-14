import { useCallback, useEffect, useState } from "react";
import type { ReactElement } from "react";
import { create } from "@bufbuild/protobuf";

import type { DeviceSelector } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  DeviceIdentifierListSchema,
  DeviceSelectorSchema,
} from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { type FirmwareRollout } from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import type { MinerModelGroup } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { useFirmwareRolloutApi } from "@/protoFleet/api/useFirmwareRolloutApi";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";
import MinerSelectionList from "@/protoFleet/components/MinerSelectionList";
import FirmwareUpdateModal from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/FirmwareUpdateModal";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import Modal from "@/shared/components/Modal";

interface SelectedFirmware {
  id: string;
  filename: string;
  size: number;
}

interface CreateFirmwareRolloutModalProps {
  open: boolean;
  /** Models that currently have a non-terminal rollout; selecting one blocks creation. */
  activeModels: Set<string>;
  onDismiss: () => void;
  onCreated: (rollout: FirmwareRollout) => void;
}

function buildDeviceSelector(allOfModel: boolean, deviceIds: string[]): DeviceSelector {
  if (allOfModel) {
    return create(DeviceSelectorSchema, { selectionType: { case: "allDevices", value: true } });
  }
  return create(DeviceSelectorSchema, {
    selectionType: {
      case: "deviceList",
      value: create(DeviceIdentifierListSchema, { deviceIdentifiers: deviceIds }),
    },
  });
}

function modelGroupLabel(group: MinerModelGroup): string {
  const name = [group.manufacturer, group.model].filter(Boolean).join(" ") || group.model;
  return `${name} (${group.count})`;
}

const CreateFirmwareRolloutModal = ({
  open,
  activeModels,
  onDismiss,
  onCreated,
}: CreateFirmwareRolloutModalProps): ReactElement => {
  const rolloutApi = useFirmwareRolloutApi();
  const firmwareApi = useFirmwareApi();
  const { getMinerModelGroups } = useMinerModelGroups();

  const [modelGroups, setModelGroups] = useState<MinerModelGroup[]>([]);
  const [name, setName] = useState("");
  const [minerModel, setMinerModel] = useState("");
  const [firmware, setFirmware] = useState<SelectedFirmware | null>(null);
  const [allOfModel, setAllOfModel] = useState(false);
  const [selectedMinerIds, setSelectedMinerIds] = useState<string[]>([]);
  const [batchSize, setBatchSize] = useState("25");
  const [batchIntervalSeconds, setBatchIntervalSeconds] = useState("300");
  const [showFirmwareModal, setShowFirmwareModal] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    let canceled = false;
    getMinerModelGroups(null)
      .then((groups) => {
        if (!canceled) setModelGroups(groups);
      })
      .catch(() => undefined);
    return () => {
      canceled = true;
    };
  }, [open, getMinerModelGroups]);

  const resetForm = useCallback(() => {
    setName("");
    setMinerModel("");
    setFirmware(null);
    setAllOfModel(false);
    setSelectedMinerIds([]);
    setBatchSize("25");
    setBatchIntervalSeconds("300");
    setError(null);
  }, []);

  const handleClose = useCallback(() => {
    if (isSubmitting) return;
    resetForm();
    onDismiss();
  }, [isSubmitting, onDismiss, resetForm]);

  const handleModelChange = (value: string) => {
    setMinerModel(value);
    setSelectedMinerIds([]);
    setAllOfModel(false);
  };

  const handleMinerSelectionChange = useCallback((state: { selectedItems: string[] }) => {
    setSelectedMinerIds(state.selectedItems);
  }, []);

  const handleFirmwareConfirm = useCallback(
    async (firmwareFileId: string) => {
      setShowFirmwareModal(false);
      try {
        const files = await firmwareApi.listFirmwareFiles();
        const match = files.find((file) => file.id === firmwareFileId);
        setFirmware(
          match
            ? { id: match.id, filename: match.filename, size: match.size }
            : { id: firmwareFileId, filename: firmwareFileId, size: 0 },
        );
      } catch {
        setFirmware({ id: firmwareFileId, filename: firmwareFileId, size: 0 });
      }
    },
    [firmwareApi],
  );

  const batchSizeNum = Number.parseInt(batchSize, 10);
  const batchIntervalNum = Number.parseInt(batchIntervalSeconds, 10);
  const estimatedInFlightBytes =
    firmware && firmware.size > 0 && Number.isFinite(batchSizeNum) ? firmware.size * batchSizeNum : 0;
  const modelHasActiveRollout = minerModel !== "" && activeModels.has(minerModel);

  const canCreate =
    name.trim() !== "" &&
    minerModel !== "" &&
    !modelHasActiveRollout &&
    firmware !== null &&
    (allOfModel || selectedMinerIds.length > 0) &&
    Number.isFinite(batchSizeNum) &&
    batchSizeNum > 0 &&
    Number.isFinite(batchIntervalNum) &&
    batchIntervalNum >= 0;

  const handleCreate = async () => {
    if (!canCreate || !firmware) return;
    setIsSubmitting(true);
    setError(null);
    try {
      const created = await rolloutApi.createRollout({
        name: name.trim(),
        firmwareFileId: firmware.id,
        minerModel,
        deviceSelector: buildDeviceSelector(allOfModel, selectedMinerIds),
        batchSize: batchSizeNum,
        batchIntervalSeconds: batchIntervalNum,
      });
      const started = await rolloutApi.startRollout(created.rolloutId);
      resetForm();
      onCreated(started);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create rollout");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <>
      <Modal
        open={open}
        onDismiss={showFirmwareModal ? undefined : handleClose}
        title="New firmware rollout"
        description="A rollout targets a single miner model and dispatches one batch at a time. Targets are frozen when it starts."
        size="large"
      >
        <div className="flex flex-col gap-4">
          {error ? (
            <div className="rounded-lg bg-intent-critical-10 p-3 text-300 text-text-critical">{error}</div>
          ) : null}

          <div className="grid gap-4 laptop:grid-cols-2">
            <label className="flex flex-col gap-1 text-300 text-text-primary">
              Rollout name
              <input
                className="rounded-lg border border-border-5 bg-surface-base px-3 py-2"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="June firmware update"
              />
            </label>
            <label className="flex flex-col gap-1 text-300 text-text-primary">
              Miner model
              <select
                className="rounded-lg border border-border-5 bg-surface-base px-3 py-2"
                value={minerModel}
                onChange={(e) => handleModelChange(e.target.value)}
              >
                <option value="">Select a miner model</option>
                {modelGroups.map((group) => (
                  <option key={`${group.manufacturer}-${group.model}`} value={group.model}>
                    {modelGroupLabel(group)}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {modelHasActiveRollout ? (
            <div className="flex items-center gap-3 rounded-lg bg-intent-warning-10 px-4 py-3 text-300 text-text-primary">
              <Alert className="shrink-0 text-intent-warning-fill" />
              <span>
                An active rollout already exists for <span className="font-semibold">{minerModel}</span>. Wait for it to
                finish, or abort it, before starting another rollout for this model.
              </span>
            </div>
          ) : minerModel ? (
            <>
              <div className="grid gap-4 laptop:grid-cols-2">
                <div className="flex flex-col gap-1 text-300 text-text-primary">
                  Firmware payload
                  <div className="flex items-center gap-3">
                    <Button
                      variant={variants.secondary}
                      size={sizes.compact}
                      text={firmware ? "Change firmware" : "Choose firmware"}
                      onClick={() => setShowFirmwareModal(true)}
                    />
                    <span className="truncate text-200 text-text-primary-50">
                      {firmware
                        ? `${firmware.filename}${firmware.size ? ` (${formatFileSize(firmware.size)})` : ""}`
                        : "None selected"}
                    </span>
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <label className="flex flex-col gap-1 text-300 text-text-primary">
                    Batch size
                    <input
                      className="rounded-lg border border-border-5 bg-surface-base px-3 py-2"
                      type="number"
                      min={1}
                      value={batchSize}
                      onChange={(e) => setBatchSize(e.target.value)}
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-300 text-text-primary">
                    Batch delay (sec)
                    <input
                      className="rounded-lg border border-border-5 bg-surface-base px-3 py-2"
                      type="number"
                      min={0}
                      value={batchIntervalSeconds}
                      onChange={(e) => setBatchIntervalSeconds(e.target.value)}
                    />
                  </label>
                </div>
              </div>

              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="text-300 font-semibold text-text-primary">Target miners</div>
                <label className="flex items-center gap-2 text-200 text-text-primary">
                  <input type="checkbox" checked={allOfModel} onChange={(e) => setAllOfModel(e.target.checked)} />
                  Target all paired {minerModel} miners
                </label>
              </div>
              {allOfModel ? (
                <div className="rounded-lg bg-surface-5 p-3 text-200 text-text-primary-50">
                  Every paired {minerModel} miner will be targeted when the rollout starts.
                </div>
              ) : (
                <>
                  <div className="flex h-[420px] flex-col overflow-hidden rounded-lg border border-border-5">
                    <MinerSelectionList
                      key={minerModel}
                      lockedModel={minerModel}
                      filterConfig={{ showTypeFilter: false }}
                      onSelectionChange={handleMinerSelectionChange}
                    />
                  </div>
                  <div className="text-200 text-text-primary-50">{selectedMinerIds.length} miner(s) selected</div>
                </>
              )}
            </>
          ) : (
            <div className="rounded-lg bg-surface-5 p-3 text-300 text-text-primary-50">
              Select a miner model to choose a firmware payload and target miners.
            </div>
          )}

          <div className="mt-2 flex flex-wrap items-center justify-between gap-3 border-t border-border-5 pt-4">
            <div className="text-300 text-text-primary-50">
              Estimated firmware bytes per batch:{" "}
              {estimatedInFlightBytes ? formatFileSize(estimatedInFlightBytes) : "—"}
            </div>
            <div className="flex gap-2">
              <Button variant={variants.secondary} text="Cancel" disabled={isSubmitting} onClick={handleClose} />
              <Button
                variant={variants.primary}
                text="Create and start rollout"
                disabled={!canCreate || isSubmitting}
                loading={isSubmitting}
                onClick={handleCreate}
              />
            </div>
          </div>
        </div>
      </Modal>

      <FirmwareUpdateModal
        open={showFirmwareModal}
        onConfirm={(firmwareFileId) => void handleFirmwareConfirm(firmwareFileId)}
        onDismiss={() => setShowFirmwareModal(false)}
      />
    </>
  );
};

export default CreateFirmwareRolloutModal;
