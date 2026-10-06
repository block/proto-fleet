import { useCallback, useMemo, useState } from "react";

import { type SiteFormValues } from "@/protoFleet/api/sites";
import {
  CA_PROVINCE_OPTIONS,
  COUNTRY_OPTIONS,
  TIMEZONE_OPTIONS,
  US_STATE_OPTIONS,
} from "@/protoFleet/features/sites/constants";
import { inferTimezone } from "@/protoFleet/features/sites/inferTimezone";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

// "create" is the initial site-details step. Continue persists the site
// (CreateSite) and hands off to ManageSiteModal in edit mode; once the site
// exists, "Edit details" reopens this modal in "edit" mode (UpdateSite).
export type SiteSettingsModalMode = "create" | "edit";

interface SiteSettingsModalCommonProps {
  open: boolean;
  initialValues: SiteFormValues;
  onDismiss: () => void;
  saving?: boolean;
}

export type SiteSettingsModalProps = SiteSettingsModalCommonProps &
  (
    | { mode: "create"; onContinue: (values: SiteFormValues) => Promise<void> | void }
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
  const [timezone, setTimezone] = useState(initialValues.timezone);
  const [notes, setNotes] = useState(initialValues.notes);
  const [capacityText, setCapacityText] = useState(
    initialValues.powerCapacityMw > 0 ? String(initialValues.powerCapacityMw) : "",
  );
  const [capacityError, setCapacityError] = useState<string | null>(null);
  const [nameError, setNameError] = useState<string | null>(null);

  // Validate-on-submit rather than gating the CTA: a disabled button can't say
  // what's wrong with the form. Collects every problem in one pass so the
  // operator sees all of them at once instead of one per click.
  const buildValues = useCallback((): SiteFormValues | null => {
    const capacity = parseCapacity(capacityText);
    const trimmedName = name.trim();
    setCapacityError(capacity === null ? "Enter a number ≥ 0" : null);
    setNameError(trimmedName === "" ? "Enter a site name" : null);
    if (capacity === null || trimmedName === "") return null;
    return {
      name: trimmedName,
      address: address.trim(),
      locationCity: city.trim(),
      locationState: state.trim(),
      postalCode: postalCode.trim(),
      country: country || "US",
      timezone: timezone,
      powerCapacityMw: capacity,
      networkConfig: initialValues.networkConfig,
      notes: notes,
    };
  }, [name, address, city, state, postalCode, country, timezone, capacityText, notes, initialValues.networkConfig]);

  // Compared against the same normalized shape buildValues produces, so
  // trailing whitespace or a capacity retyped as "12.50" doesn't read as an
  // edit. buildValues sets the field errors as a side effect, so the comparison
  // uses its own parse instead of calling it here.
  const isDirty = useMemo(() => {
    const capacity = parseCapacity(capacityText);
    return (
      name.trim() !== initialValues.name ||
      address.trim() !== initialValues.address ||
      city.trim() !== initialValues.locationCity ||
      state.trim() !== initialValues.locationState ||
      postalCode.trim() !== initialValues.postalCode ||
      (country || "US") !== (initialValues.country || "US") ||
      timezone !== initialValues.timezone ||
      capacity !== initialValues.powerCapacityMw ||
      notes !== initialValues.notes
    );
  }, [name, address, city, state, postalCode, country, timezone, capacityText, notes, initialValues]);

  const handlePrimary = useCallback(async () => {
    const values = buildValues();
    // Invalid: the errors buildValues just set are now on the fields.
    if (!values) return;
    if (props.mode === "edit") {
      // Valid but unchanged. UpdateSite would succeed and report having saved
      // something it didn't, so close instead of round-tripping. Not an error
      // state — keeping what's already there is a legitimate outcome.
      if (!isDirty) {
        onDismiss();
        return;
      }
      await props.onSave(values);
    } else {
      await props.onContinue(values);
    }
  }, [buildValues, isDirty, onDismiss, props]);

  // Only the in-flight guard. Validation and the no-diff check both run on
  // click so they can explain themselves; a disabled CTA can't.
  const primaryDisabled = saving;

  const buttons =
    props.mode === "create"
      ? [
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: onDismiss,
            disabled: saving,
            testId: "site-settings-modal-cancel",
          },
          {
            // Named for the write it performs (CreateSite) rather than "Continue"
            // — the site exists once this lands, even if the operator bails out
            // of the manage step that follows.
            text: saving ? "Creating…" : "Create site",
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
          onChange={(v) => {
            setName(v);
            if (nameError) setNameError(null);
          }}
          maxLength={255}
          required
          autoFocus
          error={nameError ?? false}
          testId="site-settings-name-input"
        />
        <Input
          id="site-settings-address"
          label="Address (optional)"
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
            if (v === country) return;
            setCountry(v);
            // State list is country-scoped — keeping a stale value (e.g.
            // "IL" when switching US → CA) would persist a code that
            // resolves to no timezone. The inferred timezone goes with
            // the now-cleared state.
            setState("");
            setTimezone("");
          }}
          forceBelow
          testId="site-settings-country-select"
        />
        <div className="grid grid-cols-2 gap-4">
          <Input
            id="site-settings-city"
            label="City (optional)"
            initValue={city}
            onChange={(v) => setCity(v)}
            maxLength={255}
            testId="site-settings-city-input"
          />
          <Select
            id="site-settings-state"
            label={country === "CA" ? "Province (optional)" : "State (optional)"}
            options={country === "CA" ? CA_PROVINCE_OPTIONS : US_STATE_OPTIONS}
            value={state}
            onChange={(v) => {
              setState(v);
              // Auto-seed timezone with the inference for the new state
              // so the operator sees the suggestion before save. They
              // can still override below (e.g. N Idaho is Pacific, not
              // the Mountain default).
              setTimezone(inferTimezone(country, v));
            }}
            forceBelow
            testId="site-settings-state-select"
          />
        </div>
        <Input
          id="site-settings-postal-code"
          label="Postal code (optional)"
          initValue={postalCode}
          onChange={(v) => setPostalCode(v)}
          maxLength={32}
          testId="site-settings-postal-code-input"
        />
        <Select
          id="site-settings-timezone"
          label="Timezone (optional)"
          options={TIMEZONE_OPTIONS}
          value={timezone}
          onChange={setTimezone}
          forceBelow
          testId="site-settings-timezone-select"
        />
        <Input
          id="site-settings-capacity"
          label="Power capacity (optional)"
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
          label="Notes (optional)"
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
