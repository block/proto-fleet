import { useCallback, useState } from "react";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import ActionErrorBanner from "@/protoFleet/features/infrastructure/components/ActionErrorBanner";
import InfraLocationFields from "@/protoFleet/features/infrastructure/components/InfraLocationFields";
import { formatDeviceType } from "@/protoFleet/features/infrastructure/deviceType";
import {
  type DriverFormValues,
  getDriverFormModule,
  getDriverTypeLabel,
  summarizeDriverConfig,
} from "@/protoFleet/features/infrastructure/driverForms";
import type {
  InfraBuildingOption,
  InfraDeviceItem,
  InfraDeviceUpdate,
} from "@/protoFleet/features/infrastructure/types";
import { variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import Switch from "@/shared/components/Switch";

interface InfraDeviceDetailModalProps {
  device: InfraDeviceItem;
  siteOptions?: string[];
  buildingOptions?: InfraBuildingOption[];
  canManage?: boolean;
  // Persist callbacks; rejections keep the modal open with the error
  // shown inline. The modal dismisses itself after success.
  onSave: (update: InfraDeviceUpdate) => Promise<void>;
  onDelete: (deviceId: string) => Promise<void>;
  onDismiss: () => void;
}

const InfraDeviceDetailModal = ({
  device,
  siteOptions = [],
  buildingOptions = [],
  canManage = true,
  onSave,
  onDelete,
  onDismiss,
}: InfraDeviceDetailModalProps) => {
  const [site, setSite] = useState(device.siteName);
  const [name, setName] = useState(device.name);
  const [building, setBuilding] = useState(device.buildingName);
  // Stays undefined until the operator flips the switch in this
  // session, so an unrelated save doesn't resend a possibly-stale
  // enabled snapshot and clobber a concurrent enable/disable.
  const [enabledOverride, setEnabledOverride] = useState<boolean | undefined>(undefined);
  const enabled = enabledOverride ?? device.enabled;
  const driverFormModule = getDriverFormModule(device.driverType);
  // null when the config is redacted (site:read callers), unparseable,
  // or the driver type has no registered module; the raw driverConfig
  // is preserved unchanged on save in that case.
  const [driverValues, setDriverValues] = useState<DriverFormValues | null>(
    () => driverFormModule?.decode(device.driverConfig) ?? null,
  );
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const isDriverConfigValid = driverValues === null || (driverFormModule?.isValid(driverValues) ?? false);
  const canSave =
    [name, site, building].every((value) => value.trim().length > 0) && isDriverConfigValid && !isSaving && !isDeleting;

  const handleDriverValueChange = useCallback((field: string, value: string) => {
    setDriverValues((current) => (current === null ? current : { ...current, [field]: value }));
  }, []);

  const handleSave = useCallback(() => {
    if (!canSave) return;
    setIsSaving(true);
    setActionError(null);
    onSave({
      id: device.id,
      name: name.trim(),
      siteName: site.trim(),
      buildingName: building.trim(),
      deviceKind: device.deviceKind,
      fanCount: device.fanCount,
      enabled: enabledOverride,
      driverType: device.driverType,
      driverConfig:
        driverValues !== null && driverFormModule ? driverFormModule.encode(driverValues) : device.driverConfig,
    })
      .then(() => {
        onDismiss();
      })
      .catch((error: unknown) => {
        setActionError(getErrorMessage(error) || "Failed to update infrastructure device.");
        setIsSaving(false);
      });
  }, [building, canSave, device, driverFormModule, driverValues, enabledOverride, name, onDismiss, onSave, site]);

  const handleDelete = useCallback(() => {
    setIsDeleting(true);
    setActionError(null);
    onDelete(device.id)
      .then(() => {
        onDismiss();
      })
      .catch((error: unknown) => {
        setActionError(getErrorMessage(error) || "Failed to delete infrastructure device.");
        setIsDeleting(false);
      });
  }, [device.id, onDelete, onDismiss]);

  const connectionTypeLabel = getDriverTypeLabel(device.driverType);
  const connectionSummary = summarizeDriverConfig(device.driverType, device.driverConfig);
  const description = formatDeviceType(device);
  const showDriverFields = canManage && driverValues !== null && driverFormModule !== undefined;

  return (
    <Modal
      open
      onDismiss={onDismiss}
      headerSpacingClassName="mt-6"
      buttons={
        canManage
          ? [
              {
                text: isDeleting ? "Deleting…" : "Delete",
                variant: variants.secondaryDanger,
                onClick: handleDelete,
                disabled: isSaving || isDeleting,
                dismissModalOnClick: false,
              },
              {
                text: isSaving ? "Saving…" : "Save",
                variant: variants.primary,
                onClick: handleSave,
                disabled: !canSave,
                dismissModalOnClick: false,
              },
            ]
          : []
      }
    >
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-1">
          <div className="text-heading-300 text-text-primary">{device.name}</div>
          {description ? <div className="text-300 text-text-primary-70">{description}</div> : null}
        </div>

        {actionError ? <ActionErrorBanner message={actionError} /> : null}

        {/* Editable fields */}
        <div className="flex flex-col gap-4">
          <Input id="device-name" label="Name" initValue={name} readOnly={!canManage} onChange={(v) => setName(v)} />
          <InfraLocationFields
            site={site}
            building={building}
            siteOptions={siteOptions}
            buildingOptions={buildingOptions}
            onSiteChange={setSite}
            onBuildingChange={setBuilding}
            disabled={!canManage}
          />
          <Input id="device-connection-type" label="Connection type" initValue={connectionTypeLabel} readOnly />
          {showDriverFields ? (
            <driverFormModule.FormFields idPrefix="device" values={driverValues} onChange={handleDriverValueChange} />
          ) : null}
          <div className="flex h-14 items-center justify-between rounded-lg border border-border-5 bg-surface-base px-4 transition duration-200 ease-in-out">
            <span className="text-300 text-text-primary">Enabled</span>
            <Switch
              ariaLabel="Enabled"
              checked={enabled}
              disabled={!canManage}
              setChecked={(next) => {
                setEnabledOverride((current) => (typeof next === "function" ? next(current ?? device.enabled) : next));
              }}
            />
          </div>
        </div>

        <Divider />

        {/* Device info */}
        <div className="flex flex-col">
          {!showDriverFields ? (
            <Row compact>
              <div className="flex w-full items-center justify-between gap-4">
                <span className="text-text-primary-70">Connection</span>
                <span className="truncate text-300 text-text-primary-70">{connectionSummary ?? "—"}</span>
              </div>
            </Row>
          ) : null}
          <Row compact divider={false}>
            <div className="flex w-full items-center justify-between">
              <span className="text-text-primary-70">Fans</span>
              <span>{device.fanCount}</span>
            </div>
          </Row>
        </div>
      </div>
    </Modal>
  );
};

export default InfraDeviceDetailModal;
