import { useCallback, useState } from "react";

import { type SiteFormValues } from "@/protoFleet/api/sites";
import { US_STATE_OPTIONS, US_TIMEZONE_OPTIONS } from "@/protoFleet/features/sites/constants";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";

// "createReturn" is the state when the operator clicks "Edit details" from
// ManageSiteModal during the create flow — they're already mid-create, so the
// CTAs read Delete (discard pending site) + Save (apply changes and return to
// the manage view) instead of Cancel + Continue.
export type SiteDetailsModalMode = "create" | "createReturn" | "edit";

interface SiteDetailsModalProps {
  open: boolean;
  mode: SiteDetailsModalMode;
  initialValues: SiteFormValues;
  // create / createReturn — parent advances to ManageSiteModal with the
  // entered values held in memory; no persistence happens here.
  onContinue?: (values: SiteFormValues) => void;
  // edit — parent commits via UpdateSite directly. Returns a promise so the
  // modal can disable Save while in flight.
  onSave?: (values: SiteFormValues) => Promise<void> | void;
  // edit — parent opens the cascade delete dialog.
  // createReturn — parent discards the in-progress create (dismisses the
  // whole modal stack).
  onDeleteRequested?: () => void;
  onDismiss: () => void;
  saving?: boolean;
}

// Parse the capacity input. Accepts integers and decimals; treats blank as
// 0 (the "unset" sentinel matched by the proto). Rejects negatives and
// non-numeric input by returning null so the form can surface an inline
// error rather than silently swallowing typos.
const parseCapacity = (input: string): number | null => {
  const trimmed = input.trim();
  if (trimmed === "") return 0;
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) return null;
  return parsed;
};

const SiteDetailsModal = ({
  open,
  mode,
  initialValues,
  onContinue,
  onSave,
  onDeleteRequested,
  onDismiss,
  saving = false,
}: SiteDetailsModalProps) => {
  const [name, setName] = useState(initialValues.name);
  const [city, setCity] = useState(initialValues.locationCity);
  const [state, setState] = useState(initialValues.locationState);
  const [timezone, setTimezone] = useState(initialValues.timezone);
  const [capacityText, setCapacityText] = useState(
    initialValues.powerCapacityMw > 0 ? String(initialValues.powerCapacityMw) : "",
  );
  const [capacityError, setCapacityError] = useState<string | null>(null);

  const buildValues = useCallback((): SiteFormValues | null => {
    const capacity = parseCapacity(capacityText);
    if (capacity === null) {
      setCapacityError("Enter a number ≥ 0");
      return null;
    }
    setCapacityError(null);
    return {
      name: name.trim(),
      locationCity: city.trim(),
      locationState: state.trim(),
      timezone: timezone.trim(),
      powerCapacityMw: capacity,
      networkConfig: initialValues.networkConfig,
    };
  }, [name, city, state, timezone, capacityText, initialValues.networkConfig]);

  const handlePrimary = useCallback(async () => {
    const values = buildValues();
    if (!values) return;
    if (mode === "edit") {
      await onSave?.(values);
    } else {
      onContinue?.(values);
    }
  }, [buildValues, mode, onContinue, onSave]);

  const nameValid = name.trim().length > 0;
  const primaryDisabled = !nameValid || saving;

  // "create" gets a plain Cancel/Continue pair; the other two modes (edit
  // and createReturn) share a Delete + Save shape because both are already
  // editing a known-in-memory site row from the operator's perspective.
  const buttons =
    mode === "create"
      ? [
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: onDismiss,
            testId: "site-details-modal-cancel",
          },
          {
            text: "Continue",
            variant: variants.primary,
            onClick: handlePrimary,
            disabled: primaryDisabled,
            dismissModalOnClick: false,
            testId: "site-details-modal-continue",
          },
        ]
      : [
          {
            text: "Delete",
            variant: variants.secondaryDanger,
            onClick: onDeleteRequested,
            disabled: saving,
            testId: "site-details-modal-delete",
          },
          {
            text: saving ? "Saving…" : "Save",
            variant: variants.primary,
            onClick: handlePrimary,
            disabled: primaryDisabled,
            dismissModalOnClick: false,
            testId: "site-details-modal-save",
          },
        ];

  const title = mode === "create" ? "Add site" : "Edit site";

  return (
    <Modal
      open={open}
      onDismiss={saving ? undefined : onDismiss}
      title={title}
      buttons={buttons}
      testId="site-details-modal"
    >
      <div className="flex flex-col gap-4 py-2">
        <Input
          id="site-details-name"
          label="Name"
          initValue={name}
          onChange={(v) => setName(v)}
          maxLength={255}
          required
          autoFocus
          testId="site-details-name-input"
        />
        <div className="grid grid-cols-2 gap-4">
          <Input
            id="site-details-city"
            label="City"
            initValue={city}
            onChange={(v) => setCity(v)}
            maxLength={255}
            testId="site-details-city-input"
          />
          <Select
            id="site-details-state"
            label="State"
            options={US_STATE_OPTIONS}
            value={state}
            onChange={setState}
            forceBelow
            testId="site-details-state-select"
          />
        </div>
        <Input
          id="site-details-capacity"
          label="Power capacity"
          initValue={capacityText}
          onChange={(v) => {
            setCapacityText(v);
            if (capacityError) setCapacityError(null);
          }}
          units="MW"
          error={capacityError ?? false}
          testId="site-details-capacity-input"
        />
        <Select
          id="site-details-timezone"
          label="Timezone"
          options={US_TIMEZONE_OPTIONS}
          value={timezone}
          onChange={setTimezone}
          forceBelow
          testId="site-details-timezone-select"
        />
      </div>
    </Modal>
  );
};

export default SiteDetailsModal;
