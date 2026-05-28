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
export type SiteSettingsModalMode = "create" | "createReturn" | "edit";

interface SiteSettingsModalCommonProps {
  open: boolean;
  initialValues: SiteFormValues;
  onDismiss: () => void;
  saving?: boolean;
}

// Discriminated by `mode` so the per-mode handler contract is type-enforced:
// create needs onContinue; createReturn needs onContinue + onDeleteRequested
// (Delete discards the pending create); edit needs onSave + onDeleteRequested
// (Delete opens the cascade dialog). A misconfigured caller fails to compile
// instead of silently no-opping the primary action.
export type SiteSettingsModalProps = SiteSettingsModalCommonProps &
  (
    | { mode: "create"; onContinue: (values: SiteFormValues) => void }
    | {
        mode: "createReturn";
        onContinue: (values: SiteFormValues) => void;
        onDeleteRequested: () => void;
      }
    | {
        mode: "edit";
        onSave: (values: SiteFormValues) => Promise<void> | void;
        onDeleteRequested: () => void;
      }
  );

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

const SiteSettingsModal = (props: SiteSettingsModalProps) => {
  const { open, initialValues, onDismiss, saving = false } = props;
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
    // Narrow on the props union so TS guarantees the right handler exists
    // for the current mode.
    if (props.mode === "edit") {
      await props.onSave(values);
    } else {
      props.onContinue(values);
    }
  }, [buildValues, props]);

  const nameValid = name.trim().length > 0;
  const primaryDisabled = !nameValid || saving;

  // "create" gets a plain Cancel/Continue pair; the other two modes (edit and
  // createReturn) share a Delete + Save shape because both are already editing
  // a known-in-memory site row from the operator's perspective. Switching on
  // props.mode (not the destructured `mode`) so TS narrows the discriminated
  // union and onDeleteRequested is type-checked.
  const buttons =
    props.mode === "create"
      ? [
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: onDismiss,
            testId: "site-settings-modal-cancel",
          },
          {
            text: "Continue",
            variant: variants.primary,
            onClick: handlePrimary,
            disabled: primaryDisabled,
            dismissModalOnClick: false,
            testId: "site-settings-modal-continue",
          },
        ]
      : [
          {
            text: "Delete",
            variant: variants.secondaryDanger,
            onClick: props.onDeleteRequested,
            disabled: saving,
            testId: "site-settings-modal-delete",
          },
          {
            text: saving ? "Saving…" : "Save",
            variant: variants.primary,
            onClick: handlePrimary,
            disabled: primaryDisabled,
            dismissModalOnClick: false,
            testId: "site-settings-modal-save",
          },
        ];

  const title = "Site settings";

  return (
    <Modal
      open={open}
      onDismiss={saving ? undefined : onDismiss}
      title={title}
      buttons={buttons}
      testId="site-settings-modal"
    >
      <div className="flex flex-col gap-4 py-2">
        <Input
          id="site-settings-name"
          label="Name"
          initValue={name}
          onChange={(v) => setName(v)}
          maxLength={255}
          required
          autoFocus
          testId="site-settings-name-input"
        />
        <div className="grid grid-cols-2 gap-4">
          <Input
            id="site-settings-city"
            label="City"
            initValue={city}
            onChange={(v) => setCity(v)}
            maxLength={255}
            testId="site-settings-city-input"
          />
          <Select
            id="site-settings-state"
            label="State"
            options={US_STATE_OPTIONS}
            value={state}
            onChange={setState}
            forceBelow
            testId="site-settings-state-select"
          />
        </div>
        <Input
          id="site-settings-capacity"
          label="Power capacity"
          initValue={capacityText}
          onChange={(v) => {
            setCapacityText(v);
            if (capacityError) setCapacityError(null);
          }}
          units="MW"
          error={capacityError ?? false}
          testId="site-settings-capacity-input"
        />
        <Select
          id="site-settings-timezone"
          label="Timezone"
          options={US_TIMEZONE_OPTIONS}
          value={timezone}
          onChange={setTimezone}
          forceBelow
          testId="site-settings-timezone-select"
        />
      </div>
    </Modal>
  );
};

export default SiteSettingsModal;
