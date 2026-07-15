import { useMemo, useState } from "react";

import {
  curtailmentNumericFieldLimits,
  parseOptionalUint32Field,
} from "@/protoFleet/features/energy/curtailmentNumericFields";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

export interface FacilityFanDeviceOption {
  id: string;
  siteId: string;
  siteName: string;
  buildingName: string;
  name: string;
  deviceKind: string;
  fanCount?: number;
  enabled: boolean;
}

export interface FacilityFanSelectionValue {
  selectedDeviceIds: string[];
  fanOffDelaySec: string;
  fanRestoreDelaySec: string;
}

interface FacilityFanSelectionModalProps {
  devices: FacilityFanDeviceOption[];
  initialSelectedDeviceIds: string[];
  initialFanOffDelaySec: string;
  initialFanRestoreDelaySec: string;
  isLoading?: boolean;
  loadError?: string | null;
  onDismiss: () => void;
  onApply: (value: FacilityFanSelectionValue) => void;
  onRetry?: () => void;
}

interface DeviceGroup {
  siteId: string;
  siteName: string;
  devices: FacilityFanDeviceOption[];
}

function formatCount(count: number, singular: string): string {
  return `${count} ${count === 1 ? singular : `${singular}s`}`;
}

function formatDeviceKind(device: FacilityFanDeviceOption): string {
  if (device.deviceKind === "fan_group") {
    return device.fanCount && device.fanCount > 1 ? `Fan group · ${formatCount(device.fanCount, "fan")}` : "Fan group";
  }

  return "Single fan";
}

function groupDevicesBySite(devices: FacilityFanDeviceOption[]): DeviceGroup[] {
  const groups = new Map<string, DeviceGroup>();

  for (const device of devices) {
    const siteName = device.siteName.trim() || `Site ${device.siteId}`;
    const existing = groups.get(device.siteId);
    if (existing) {
      existing.devices.push(device);
      continue;
    }

    groups.set(device.siteId, {
      siteId: device.siteId,
      siteName,
      devices: [device],
    });
  }

  return [...groups.values()]
    .sort((left, right) => left.siteName.localeCompare(right.siteName))
    .map((group) => ({
      ...group,
      devices: group.devices.sort(
        (left, right) => left.buildingName.localeCompare(right.buildingName) || left.name.localeCompare(right.name),
      ),
    }));
}

function BehaviorMode({ value }: { value: string }) {
  return (
    <div className="flex h-14 flex-col justify-center rounded-lg border border-border-5 bg-surface-base px-4">
      <span className="text-200 text-text-primary-50">Mode</span>
      <span className="text-300 text-text-primary">{value}</span>
    </div>
  );
}

function FacilityFanSelectionModal({
  devices,
  initialSelectedDeviceIds,
  initialFanOffDelaySec,
  initialFanRestoreDelaySec,
  isLoading = false,
  loadError = null,
  onDismiss,
  onApply,
  onRetry,
}: FacilityFanSelectionModalProps) {
  const [selectedDeviceIds, setSelectedDeviceIds] = useState(() => new Set(initialSelectedDeviceIds));
  const [fanOffDelaySec, setFanOffDelaySec] = useState(initialFanOffDelaySec);
  const [fanRestoreDelaySec, setFanRestoreDelaySec] = useState(initialFanRestoreDelaySec);
  const [showValidationErrors, setShowValidationErrors] = useState(false);
  const deviceGroups = useMemo(() => groupDevicesBySite(devices), [devices]);
  const selectableDeviceIds = useMemo(
    () => devices.filter((device) => device.enabled).map((device) => device.id),
    [devices],
  );
  const fanOffDelay = parseOptionalUint32Field(fanOffDelaySec, {
    label: "fan-off delay",
    max: curtailmentNumericFieldLimits.fanDelaySec,
  });
  const fanRestoreDelay = parseOptionalUint32Field(fanRestoreDelaySec, {
    label: "fan restore delay",
    max: curtailmentNumericFieldLimits.fanDelaySec,
  });

  const toggleDevice = (device: FacilityFanDeviceOption) => {
    if (!device.enabled) {
      return;
    }

    setSelectedDeviceIds((current) => {
      const next = new Set(current);
      if (next.has(device.id)) {
        next.delete(device.id);
      } else {
        next.add(device.id);
      }
      return next;
    });
  };

  const handleApply = () => {
    if (fanOffDelay.error || fanRestoreDelay.error) {
      setShowValidationErrors(true);
      return;
    }

    onApply({
      selectedDeviceIds: [...selectedDeviceIds],
      fanOffDelaySec: fanOffDelaySec.trim(),
      fanRestoreDelaySec: fanRestoreDelaySec.trim(),
    });
  };

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Fan behavior during curtailment"
      description={`${formatCount(devices.length, "device")} in scope`}
      surfaceClassName="!w-[min(calc(100vw-(--spacing(4))),600px)]"
      testId="facility-fan-selection-modal"
      divider={false}
      fixedFooter={
        !isLoading && !loadError && selectableDeviceIds.length > 0 ? (
          <ModalSelectAllFooter
            label={`${formatCount(selectedDeviceIds.size, "device")} selected`}
            onSelectAll={() => setSelectedDeviceIds(new Set(selectableDeviceIds))}
            onSelectNone={() => setSelectedDeviceIds(new Set())}
          />
        ) : null
      }
      buttons={[
        {
          text: "Apply",
          variant: variants.primary,
          onClick: handleApply,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-8" data-testid="facility-fan-modal-scroll-area">
        <section className="grid gap-3">
          <div>
            <h3 className="text-heading-100 text-text-primary">Curtail behavior</h3>
            <p className="text-300 text-text-primary-50">Fans turn off after miners are confirmed curtailed.</p>
          </div>
          <div className="grid gap-3 tablet:grid-cols-2">
            <BehaviorMode value="Turn off" />
            <Input
              id="facility-fan-off-delay"
              label="Delay after miners curtail (sec)"
              initValue={fanOffDelaySec}
              inputMode="numeric"
              error={showValidationErrors ? fanOffDelay.error : undefined}
              testId="facility-fan-off-delay"
              onChange={setFanOffDelaySec}
            />
          </div>
        </section>

        <section className="grid gap-3">
          <div>
            <h3 className="text-heading-100 text-text-primary">Restore behavior</h3>
            <p className="text-300 text-text-primary-50">Fans turn on before miners begin restoring.</p>
          </div>
          <div className="grid gap-3 tablet:grid-cols-2">
            <BehaviorMode value="Turn on" />
            <Input
              id="facility-fan-restore-delay"
              label="Delay before miners restore (sec)"
              initValue={fanRestoreDelaySec}
              inputMode="numeric"
              error={showValidationErrors ? fanRestoreDelay.error : undefined}
              testId="facility-fan-restore-delay"
              onChange={setFanRestoreDelaySec}
            />
          </div>
        </section>

        <section className="grid gap-3">
          <h3 className="text-heading-100 text-text-primary">Devices</h3>

          {isLoading ? (
            <div className="flex min-h-32 items-center justify-center" data-testid="facility-fan-devices-loading">
              <ProgressCircular indeterminate />
            </div>
          ) : loadError ? (
            <div className="rounded-lg bg-intent-critical-10 px-4 py-4 text-300 text-text-critical">
              <p>{loadError}</p>
              {onRetry ? (
                <button className="mt-2 cursor-pointer text-emphasis-300 underline" type="button" onClick={onRetry}>
                  Try again
                </button>
              ) : null}
            </div>
          ) : deviceGroups.length === 0 ? (
            <div className="rounded-lg bg-surface-overlay px-5 py-6 text-300 text-text-primary-70">
              <p>No infrastructure fan devices are available in this site scope.</p>
              <a
                className="mt-2 inline-block text-emphasis-300 text-core-accent-fill underline"
                href="/fleet/infrastructure"
              >
                Add devices in Fleet Infrastructure
              </a>
            </div>
          ) : (
            <div className="grid gap-6">
              {deviceGroups.map((group) => (
                <div key={group.siteId} className="grid gap-1">
                  <h4 className="px-3 text-emphasis-300 text-text-primary-70">{group.siteName}</h4>
                  <div className="divide-y divide-border-5">
                    {group.devices.map((device) => (
                      <label
                        key={device.id}
                        className={
                          device.enabled
                            ? "flex cursor-pointer items-center gap-4 rounded-lg px-3 py-3 hover:bg-core-primary-5"
                            : "flex cursor-not-allowed items-center gap-4 rounded-lg px-3 py-3 opacity-60"
                        }
                        data-testid={`facility-fan-device-${device.id}`}
                      >
                        <Checkbox
                          checked={selectedDeviceIds.has(device.id)}
                          disabled={!device.enabled}
                          onChange={() => toggleDevice(device)}
                        />
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-emphasis-300 text-text-primary">{device.name}</span>
                          <span className="block truncate text-300 text-text-primary-50">
                            {[device.buildingName, group.siteName].filter(Boolean).join(" · ")}
                          </span>
                        </span>
                        <span className="flex shrink-0 flex-col items-end gap-1 text-300 text-text-primary-70">
                          <span>{formatDeviceKind(device)}</span>
                          {!device.enabled ? (
                            <span className="rounded-full bg-intent-warning-10 px-2 py-0.5 text-200 text-intent-warning-text">
                              Disabled
                            </span>
                          ) : null}
                        </span>
                      </label>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>
    </Modal>
  );
}

export default FacilityFanSelectionModal;
