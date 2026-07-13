import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { CohortDevice } from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { MinerModelGroup } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";
import MinerSelectionList, { type MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";
import { durationToExpiresAt, type ExpiryPreset } from "@/protoFleet/features/cohorts/utils";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import { DatePickerField } from "@/shared/components/DatePicker";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type CreateMode = "count" | "explicit" | "group";

interface CohortModalProps {
  show: boolean;
  onDismiss: () => void;
  onSuccess: () => void;
}

const createModeOptions = [
  { value: "count", label: "Reserve count" },
  { value: "explicit", label: "Selected miners" },
  { value: "group", label: "Group" },
];

const expiryPresetOptions = [
  { value: "none", label: "No expiration" },
  { value: "4h", label: "4 hours" },
  { value: "8h", label: "8 hours" },
  { value: "24h", label: "24 hours" },
  { value: "3d", label: "3 days" },
  { value: "7d", label: "7 days" },
  { value: "custom", label: "Custom" },
];

const hourOptions = Array.from({ length: 24 }, (_, hour) => {
  const value = hour.toString().padStart(2, "0");
  return { value, label: value };
});

const minuteOptions = Array.from({ length: 12 }, (_, index) => {
  const value = (index * 5).toString().padStart(2, "0");
  return { value, label: value };
});

const roundToFiveMinutes = (date: Date) => {
  const next = new Date(date);
  const remainder = next.getMinutes() % 5;
  if (remainder !== 0) {
    next.setMinutes(next.getMinutes() + (5 - remainder));
  }
  next.setSeconds(0, 0);
  return next;
};

const combineDateAndTime = (date: Date | undefined, hour: string, minute: string) => {
  if (!date) return undefined;
  const next = new Date(date);
  next.setHours(Number.parseInt(hour, 10), Number.parseInt(minute, 10), 0, 0);
  return next;
};

const isPastDate = (date: Date) => {
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  return date < today;
};

const optionalBigInt = (value: string, label: string) => {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  const parsed = BigInt(trimmed);
  if (parsed <= 0n) {
    throw new Error(`${label} must be greater than zero`);
  }
  return parsed;
};

const formatFirmwareOption = (file: FirmwareFileInfo) => ({
  value: file.id,
  label: file.filename || file.id,
  description: `${file.target_manufacturer} ${file.target_model}`.trim() || file.id,
});

const matchesFirmwareTarget = (file: FirmwareFileInfo, manufacturer: string, model: string) =>
  file.target_manufacturer === manufacturer && file.target_model === model;

const matchesCohortTarget = (item: { manufacturer: string; model: string }, manufacturer: string, model: string) =>
  item.manufacturer === manufacturer && item.model === model;

const cohortDeviceMatchesTarget = (device: CohortDevice, manufacturer: string, model: string) =>
  device.display?.manufacturer === manufacturer && device.display?.model === model;

const formatGroupOption = (group: DeviceSet) => ({
  value: group.id.toString(),
  label: group.label,
  description: `${group.deviceCount} ${group.deviceCount === 1 ? "miner" : "miners"}`,
});

const CohortModal = ({ show, onDismiss, onSuccess }: CohortModalProps) => {
  const { createCohort, listAllDevices } = useCohortApi();
  const { listFirmwareFiles } = useFirmwareApi();
  const { listGroups } = useDeviceSets();
  const { getMinerModelGroups } = useMinerModelGroups();
  const selectionRef = useRef<MinerSelectionListHandle>(null);

  const [mode, setMode] = useState<CreateMode>("count");
  const [label, setLabel] = useState("");
  const [purpose, setPurpose] = useState("");
  const [expiryPreset, setExpiryPreset] = useState<ExpiryPreset>("24h");
  const initialCustomExpiry = useMemo(() => roundToFiveMinutes(new Date()), []);
  const [customExpiryDate, setCustomExpiryDate] = useState<Date | undefined>(initialCustomExpiry);
  const [customExpiryHour, setCustomExpiryHour] = useState(initialCustomExpiry.getHours().toString().padStart(2, "0"));
  const [customExpiryMinute, setCustomExpiryMinute] = useState(
    initialCustomExpiry.getMinutes().toString().padStart(2, "0"),
  );
  const [firmwareFileId, setFirmwareFileId] = useState("");
  const [sourceDeviceSetId, setSourceDeviceSetId] = useState("");
  const [count, setCount] = useState("1");
  const [product, setProduct] = useState("");
  const [model, setModel] = useState("");
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [groups, setGroups] = useState<DeviceSet[]>([]);
  const [modelGroups, setModelGroups] = useState<MinerModelGroup[]>([]);
  const [cohortDevices, setCohortDevices] = useState<CohortDevice[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");

  const reset = useCallback(() => {
    setMode("count");
    setLabel("");
    setPurpose("");
    setExpiryPreset("24h");
    const nextCustomExpiry = roundToFiveMinutes(new Date());
    setCustomExpiryDate(nextCustomExpiry);
    setCustomExpiryHour(nextCustomExpiry.getHours().toString().padStart(2, "0"));
    setCustomExpiryMinute(nextCustomExpiry.getMinutes().toString().padStart(2, "0"));
    setFirmwareFileId("");
    setSourceDeviceSetId("");
    setCount("1");
    setProduct("");
    setModel("");
    setErrorMsg("");
  }, []);

  useEffect(() => {
    if (!show) return;

    let cancelled = false;
    listFirmwareFiles()
      .then((files) => {
        if (!cancelled) setFirmwareFiles(files);
      })
      .catch((error) => {
        if (!cancelled) {
          setFirmwareFiles([]);
          pushToast({ message: error?.message || "Couldn't load firmware files", status: STATUSES.error });
        }
      });

    void listGroups({
      onSuccess: (nextGroups) => {
        if (!cancelled) setGroups(nextGroups);
      },
      onError: (message) => {
        if (!cancelled) pushToast({ message: message || "Couldn't load groups", status: STATUSES.error });
      },
    });

    void getMinerModelGroups(null)
      .then((nextGroups) => {
        if (!cancelled) setModelGroups(nextGroups);
      })
      .catch((error) => {
        if (!cancelled) pushToast({ message: error?.message || "Couldn't load miner models", status: STATUSES.error });
      });

    void listAllDevices()
      .then((nextDevices) => {
        if (!cancelled) setCohortDevices(nextDevices);
      })
      .catch((error) => {
        if (!cancelled) {
          setCohortDevices([]);
          pushToast({ message: error?.message || "Couldn't load cohort miner availability", status: STATUSES.error });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [getMinerModelGroups, listAllDevices, listFirmwareFiles, listGroups, show]);

  const firmwareTarget = useMemo(() => {
    const manufacturer = product.trim();
    const targetModel = model.trim();
    return manufacturer && targetModel ? { manufacturer, model: targetModel } : null;
  }, [model, product]);

  const compatibleFirmwareFiles = useMemo(
    () =>
      firmwareTarget
        ? firmwareFiles.filter((file) => matchesFirmwareTarget(file, firmwareTarget.manufacturer, firmwareTarget.model))
        : [],
    [firmwareFiles, firmwareTarget],
  );

  const firmwareOptions = useMemo(
    () => [{ value: "", label: "No firmware" }, ...compatibleFirmwareFiles.map(formatFirmwareOption)],
    [compatibleFirmwareFiles],
  );
  const selectedFirmwareFileId = firmwareOptions.some((option) => option.value === firmwareFileId)
    ? firmwareFileId
    : "";

  const productOptions = useMemo(() => {
    const manufacturers = [...new Set(modelGroups.map((group) => group.manufacturer).filter(Boolean))].sort();
    return [
      { value: "", label: "Select product" },
      ...manufacturers.map((manufacturer) => ({ value: manufacturer, label: manufacturer })),
    ];
  }, [modelGroups]);

  const modelOptions = useMemo(() => {
    const models = [
      ...new Set(
        modelGroups
          .filter((group) => !product || group.manufacturer === product)
          .map((group) => group.model)
          .filter(Boolean),
      ),
    ].sort();
    return [
      { value: "", label: "Select model" },
      ...models.map((modelName) => ({ value: modelName, label: modelName })),
    ];
  }, [modelGroups, product]);

  const groupOptions = useMemo(() => groups.map(formatGroupOption), [groups]);

  const hasCohortTarget = product.trim() !== "" && model.trim() !== "";
  const assignableDevices = useMemo(() => {
    const manufacturer = product.trim();
    const targetModel = model.trim();
    if (!manufacturer || !targetModel) return [];
    return cohortDevices.filter(
      (device) => device.effectiveCohort?.isDefault && cohortDeviceMatchesTarget(device, manufacturer, targetModel),
    );
  }, [cohortDevices, model, product]);
  const assignableDeviceIds = useMemo(
    () => new Set(assignableDevices.map((device) => device.deviceIdentifier)),
    [assignableDevices],
  );
  const fixedModelFilter = useMemo(() => {
    const targetModel = model.trim();
    return targetModel ? [targetModel] : [];
  }, [model]);

  const canSubmit = useMemo(
    () => label.trim().length > 0 && hasCohortTarget && !isSubmitting,
    [hasCohortTarget, isSubmitting, label],
  );

  const customExpiresAt = useMemo(
    () => combineDateAndTime(customExpiryDate, customExpiryHour, customExpiryMinute),
    [customExpiryDate, customExpiryHour, customExpiryMinute],
  );
  const customExpiresAtLabel = customExpiresAt ? customExpiresAt.toLocaleString() : "Select an expiration";

  const handleCreate = useCallback(() => {
    const trimmedLabel = label.trim();
    if (!trimmedLabel) {
      setErrorMsg("Label is required");
      return;
    }
    if (!product.trim()) {
      setErrorMsg("Product is required");
      return;
    }
    if (!model.trim()) {
      setErrorMsg("Model is required");
      return;
    }

    let parsedCount = Number.parseInt(count, 10);
    if (mode === "count" && (!Number.isFinite(parsedCount) || parsedCount <= 0 || parsedCount > 10000)) {
      setErrorMsg("Count must be between 1 and 10000");
      return;
    }
    if (mode !== "count") {
      parsedCount = 0;
    }

    const ids = mode === "explicit" ? (selectionRef.current?.getSelection().selectedItems ?? []) : [];
    if (mode === "explicit" && ids.length === 0) {
      setErrorMsg("Select at least one miner");
      return;
    }

    setIsSubmitting(true);
    setErrorMsg("");

    try {
      const parsedSourceDeviceSetId = mode === "group" ? optionalBigInt(sourceDeviceSetId, "Group") : undefined;
      if (mode === "group" && parsedSourceDeviceSetId === undefined) {
        throw new Error("Group is required");
      }
      const expiresAt = expiryPreset === "custom" ? customExpiresAt : durationToExpiresAt(expiryPreset, "1", "days");
      if (expiryPreset === "custom" && !expiresAt) {
        throw new Error("Expiration is required");
      }
      if (expiresAt && expiresAt.getTime() <= Date.now()) {
        throw new Error("Expiration must be in the future");
      }

      void createCohort({
        label: trimmedLabel,
        purpose: purpose.trim() || "Reservation",
        claimOwnership: true,
        expiresAt,
        desiredFirmwareFileId: selectedFirmwareFileId,
        ...(mode === "count"
          ? {
              selector: {
                count: parsedCount,
                product: product.trim(),
                model: model.trim(),
              },
            }
          : {}),
        ...(mode === "explicit" ? { deviceIdentifiers: ids } : {}),
        ...(mode === "group" ? { sourceDeviceSetId: parsedSourceDeviceSetId } : {}),
        onSuccess: () => {
          pushToast({ message: `Cohort "${trimmedLabel}" created`, status: STATUSES.success });
          reset();
          onSuccess();
          onDismiss();
        },
        onError: (message) => setErrorMsg(message || "Couldn't create cohort"),
        onFinally: () => setIsSubmitting(false),
      });
    } catch (error) {
      setIsSubmitting(false);
      setErrorMsg(error instanceof Error ? error.message : "Couldn't create cohort");
    }
  }, [
    count,
    createCohort,
    customExpiresAt,
    expiryPreset,
    label,
    mode,
    model,
    onDismiss,
    onSuccess,
    product,
    purpose,
    reset,
    selectedFirmwareFileId,
    sourceDeviceSetId,
  ]);

  if (!show) return null;

  return (
    <Modal
      onDismiss={onDismiss}
      open={show}
      size={mode === "explicit" ? "large" : "standard"}
      className={mode === "explicit" ? "flex !h-[calc(100vh-(--spacing(16)))] flex-col !overflow-hidden" : undefined}
      bodyClassName={mode === "explicit" ? "flex flex-1 min-h-0 flex-col overflow-hidden" : undefined}
      title="Create cohort"
      buttons={[
        {
          text: "Create",
          onClick: handleCreate,
          variant: variants.primary,
          loading: isSubmitting,
          disabled: !canSubmit,
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
    >
      <div className={mode === "explicit" ? "mt-4 flex min-h-0 flex-1 flex-col gap-4" : "mt-4 flex flex-col gap-4"}>
        {errorMsg ? <Callout intent="danger" prefixIcon={<Alert />} testId="cohort-error" title={errorMsg} /> : null}

        <Input id="cohort-label" label="Label" initValue={label} onChange={(value) => setLabel(value)} required />
        <Input id="cohort-purpose" label="Purpose" initValue={purpose} onChange={(value) => setPurpose(value)} />
        <div className="grid gap-4 tablet:grid-cols-2">
          <Select
            id="cohort-product"
            label="Product"
            options={productOptions}
            value={product}
            onChange={(value) => {
              setProduct(value);
              setModel("");
              setFirmwareFileId("");
            }}
            forceBelow
          />
          <Select
            id="cohort-model"
            label="Model"
            options={modelOptions}
            value={model}
            onChange={(value) => {
              setModel(value);
              setFirmwareFileId("");
            }}
            disabled={!product}
            forceBelow
          />
        </div>

        <div className="grid gap-4 tablet:grid-cols-2">
          <Select
            id="cohort-expiry-preset"
            label="Expiration"
            options={expiryPresetOptions}
            value={expiryPreset}
            onChange={(value) => setExpiryPreset(value as ExpiryPreset)}
            forceBelow
          />
          <Select
            id="cohort-firmware-file-id"
            label="Firmware"
            options={firmwareOptions}
            value={selectedFirmwareFileId}
            onChange={setFirmwareFileId}
            disabled={!hasCohortTarget}
            forceBelow
          />
        </div>

        {expiryPreset === "custom" ? (
          <div className="flex flex-col gap-4">
            <DatePickerField
              id="cohort-custom-expiry-date"
              label="Date"
              labelPlacement="floating"
              selectedDate={customExpiryDate}
              onSelectedDateChange={(date) => {
                setCustomExpiryDate(date);
                setErrorMsg("");
              }}
              isDateDisabled={isPastDate}
              popoverRenderMode="portal-scrolling"
              testId="cohort-custom-expiry-date"
            />
            <div className="grid gap-4 tablet:grid-cols-2">
              <Select
                id="cohort-custom-expiry-hour"
                label="Hour"
                options={hourOptions}
                value={customExpiryHour}
                onChange={(value) => {
                  setCustomExpiryHour(value);
                  setErrorMsg("");
                }}
                forceBelow
              />
              <Select
                id="cohort-custom-expiry-minute"
                label="Minute"
                options={minuteOptions}
                value={customExpiryMinute}
                onChange={(value) => {
                  setCustomExpiryMinute(value);
                  setErrorMsg("");
                }}
                forceBelow
              />
            </div>
            <div className="rounded-lg bg-core-primary-5 px-4 py-3">
              <div className="text-200 text-text-primary-70">Expiration</div>
              <div className="mt-1 text-emphasis-300 text-text-primary">{customExpiresAtLabel}</div>
            </div>
          </div>
        ) : null}

        <Select
          id="cohort-create-mode"
          label="Initial members"
          options={createModeOptions}
          value={mode}
          onChange={(v) => setMode(v as CreateMode)}
          forceBelow
        />

        {mode === "count" ? (
          <Input
            id="cohort-count"
            label="Count"
            initValue={count}
            onChange={(value) => setCount(value)}
            inputMode="numeric"
            type="number"
            required
          />
        ) : null}

        {mode === "explicit" ? (
          <div className="flex min-h-[360px] flex-1 flex-col overflow-hidden rounded-lg border border-border-5 p-3">
            <MinerSelectionList
              key={`${product}:${model}`}
              ref={selectionRef}
              filterConfig={{
                showTypeFilter: false,
                showRackFilter: true,
                showGroupFilter: true,
                showSiteFilter: true,
              }}
              fixedModels={fixedModelFilter}
              showSelectAllFooter={false}
              isRowVisible={(item) =>
                hasCohortTarget &&
                assignableDeviceIds.has(item.deviceIdentifier) &&
                matchesCohortTarget(item, product, model)
              }
              noDataElement={
                <div className="py-10 text-center text-300 text-text-primary-70">
                  {hasCohortTarget
                    ? "No available miners match this product and model."
                    : "Select a product and model."}
                </div>
              }
              visibleTotal={assignableDevices.length}
            />
          </div>
        ) : null}

        {mode === "group" ? (
          <Select
            id="cohort-source-device-set-id"
            label="Group"
            options={groupOptions}
            value={sourceDeviceSetId}
            onChange={setSourceDeviceSetId}
            forceBelow
          />
        ) : null}
      </div>
    </Modal>
  );
};

export default CohortModal;
