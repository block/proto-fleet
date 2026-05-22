import { useCallback, useState } from "react";

import { type BuildingFormValues } from "@/protoFleet/api/buildings";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";

// "createReturn" mirrors SiteDetailsModal — when BuildingDetailsModal is
// stacked on ManageBuildingModal in create flow, the CTAs read Delete
// (discard pending create) + Save (apply and return to manage). For
// PR 3 the create flow lands directly via "Save" from ManageSiteModal
// (no deferred-commit "Continue" gate like sites have), so create mode
// uses the same Save CTA — there's no separate "createReturn" mode here.
export type BuildingDetailsModalMode = "create" | "edit";

interface BuildingDetailsModalCommonProps {
  open: boolean;
  initialValues: BuildingFormValues;
  // The parent site context is required for create — buildings always
  // live under a site in the UI. Passed through so the modal can echo
  // the site label in the header copy.
  parentSiteLabel?: string;
  onDismiss: () => void;
  saving?: boolean;
}

// Discriminated union mirrors SiteDetailsModal. Edit gets onSave +
// onDeleteRequested; create gets onSave only (Delete is meaningless
// before the row exists).
export type BuildingDetailsModalProps = BuildingDetailsModalCommonProps &
  (
    | { mode: "create"; onSave: (values: BuildingFormValues) => Promise<void> | void }
    | {
        mode: "edit";
        onSave: (values: BuildingFormValues) => Promise<void> | void;
        onDeleteRequested: () => void;
      }
  );

// Building type is deferred (proto `building_type` enum has not
// shipped — see plan §898). We surface a disabled dropdown so the
// design intent is visible in the modal without storing a value.
const BUILDING_TYPE_OPTIONS = [{ value: "", label: "—" }];

// Parse positive-number form input. Blank → 0 (treated as "unset" by
// the server). Negative / non-numeric returns null so the form can
// surface an inline error.
const parseNonNegative = (input: string): number | null => {
  const trimmed = input.trim();
  if (trimmed === "") return 0;
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) return null;
  return parsed;
};

const BuildingDetailsModal = (props: BuildingDetailsModalProps) => {
  const { open, initialValues, parentSiteLabel, onDismiss, saving = false } = props;
  const [name, setName] = useState(initialValues.name);
  const [powerText, setPowerText] = useState(
    initialValues.powerCapacityMw > 0 ? String(initialValues.powerCapacityMw) : "",
  );
  const [overheadText, setOverheadText] = useState(
    initialValues.overheadKw > 0 ? String(initialValues.overheadKw) : "",
  );
  const [powerError, setPowerError] = useState<string | null>(null);
  const [overheadError, setOverheadError] = useState<string | null>(null);

  const buildValues = useCallback((): BuildingFormValues | null => {
    const power = parseNonNegative(powerText);
    if (power === null) {
      setPowerError("Enter a number ≥ 0");
      return null;
    }
    setPowerError(null);

    const overhead = parseNonNegative(overheadText);
    if (overhead === null) {
      setOverheadError("Enter a number ≥ 0");
      return null;
    }
    setOverheadError(null);

    return {
      name: name.trim(),
      // description / aisles / racksPerAisle are not surfaced in the
      // details form — they're either deferred (description) or owned
      // by ManageBuildingModal (aisles, racksPerAisle). Preserve the
      // initial values so edit-mode round trips don't clobber them.
      description: initialValues.description,
      powerCapacityMw: power,
      overheadKw: overhead,
      aisles: initialValues.aisles,
      racksPerAisle: initialValues.racksPerAisle,
    };
  }, [name, powerText, overheadText, initialValues.description, initialValues.aisles, initialValues.racksPerAisle]);

  const handlePrimary = useCallback(async () => {
    const values = buildValues();
    if (!values) return;
    await props.onSave(values);
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
            disabled: saving,
            testId: "building-details-modal-cancel",
          },
          {
            text: saving ? "Saving…" : "Save",
            variant: variants.primary,
            onClick: handlePrimary,
            disabled: primaryDisabled,
            dismissModalOnClick: false,
            testId: "building-details-modal-save",
          },
        ]
      : [
          {
            text: "Delete",
            variant: variants.secondaryDanger,
            onClick: props.onDeleteRequested,
            disabled: saving,
            testId: "building-details-modal-delete",
          },
          {
            text: saving ? "Saving…" : "Save",
            variant: variants.primary,
            onClick: handlePrimary,
            disabled: primaryDisabled,
            dismissModalOnClick: false,
            testId: "building-details-modal-save",
          },
        ];

  const title = props.mode === "create" ? "Add building" : "Edit building";
  const description = parentSiteLabel ? `in ${parentSiteLabel}` : undefined;

  return (
    <Modal
      open={open}
      onDismiss={saving ? undefined : onDismiss}
      title={title}
      description={description}
      buttons={buttons}
      testId="building-details-modal"
    >
      <div className="flex flex-col gap-4 py-2">
        <Input
          id="building-details-name"
          label="Name"
          initValue={name}
          onChange={(v) => setName(v)}
          maxLength={255}
          required
          autoFocus
          testId="building-details-name-input"
        />
        <Select
          id="building-details-type"
          label="Type"
          options={BUILDING_TYPE_OPTIONS}
          value=""
          onChange={() => undefined}
          disabled
          forceBelow
          testId="building-details-type-select"
        />
        <Input
          id="building-details-power"
          label="Power capacity"
          initValue={powerText}
          onChange={(v) => {
            setPowerText(v);
            if (powerError) setPowerError(null);
          }}
          units="MW"
          error={powerError ?? false}
          testId="building-details-power-input"
        />
        <Input
          id="building-details-overhead"
          label="Overhead"
          initValue={overheadText}
          onChange={(v) => {
            setOverheadText(v);
            if (overheadError) setOverheadError(null);
          }}
          units="kW"
          error={overheadError ?? false}
          testId="building-details-overhead-input"
        />
      </div>
    </Modal>
  );
};

export default BuildingDetailsModal;
