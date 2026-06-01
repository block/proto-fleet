import { useCallback, useState } from "react";

import { type SiteFormValues } from "@/protoFleet/api/sites";
import { CA_PROVINCE_OPTIONS, COUNTRY_OPTIONS, US_STATE_OPTIONS } from "@/protoFleet/features/sites/constants";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

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
  const [address, setAddress] = useState(initialValues.address);
  const [city, setCity] = useState(initialValues.locationCity);
  const [state, setState] = useState(initialValues.locationState);
  const [postalCode, setPostalCode] = useState(initialValues.postalCode);
  const [country, setCountry] = useState(initialValues.country || "US");
  const [notes, setNotes] = useState(initialValues.notes);
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
      address: address.trim(),
      locationCity: city.trim(),
      locationState: state.trim(),
      postalCode: postalCode.trim(),
      country: country || "US",
      powerCapacityMw: capacity,
      networkConfig: initialValues.networkConfig,
      notes: notes,
    };
  }, [name, address, city, state, postalCode, country, capacityText, notes, initialValues.networkConfig]);

  const handlePrimary = useCallback(async () => {
    const values = buildValues();
    if (!values) return;
    if (props.mode === "edit") {
      await props.onSave(values);
    } else {
      props.onContinue(values);
    }
  }, [buildValues, props]);

  const nameValid = name.trim().length > 0;
  const primaryDisabled = !nameValid || saving;

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
        <Input
          id="site-settings-address"
          label="Address"
          initValue={address}
          onChange={(v) => setAddress(v)}
          maxLength={255}
          testId="site-settings-address-input"
        />
        <Select
          id="site-settings-country"
          label="Country"
          options={COUNTRY_OPTIONS}
          value={country}
          onChange={(v) => {
            setCountry(v);
            // State list is country-scoped — keeping a stale value (e.g.
            // "IL" when switching US → CA) would persist a code that
            // resolves to no timezone.
            setState("");
          }}
          forceBelow
          testId="site-settings-country-select"
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
            label={country === "CA" ? "Province" : "State"}
            options={country === "CA" ? CA_PROVINCE_OPTIONS : US_STATE_OPTIONS}
            value={state}
            onChange={setState}
            forceBelow
            testId="site-settings-state-select"
          />
        </div>
        <Input
          id="site-settings-postal-code"
          label="Postal code"
          initValue={postalCode}
          onChange={(v) => setPostalCode(v)}
          maxLength={32}
          testId="site-settings-postal-code-input"
        />
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
        <Textarea
          id="site-settings-notes"
          label="Notes"
          initValue={notes}
          onChange={(v) => setNotes(v)}
          rows={4}
          maxLength={4096}
          testId="site-settings-notes-input"
        />
      </div>
    </Modal>
  );
};

export default SiteSettingsModal;
