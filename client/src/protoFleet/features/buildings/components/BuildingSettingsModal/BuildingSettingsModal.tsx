import { useCallback, useState } from "react";

import { type BuildingFormValues } from "@/protoFleet/api/buildings";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";

// Create vs. edit drives the CTA shape: create gets Cancel + Save, edit gets
// Delete + Save (mirroring RackSettingsModal's Continue-vs-Save split and
// SiteSettingsModal's create/edit footprint).
export type BuildingSettingsModalMode = "create" | "edit";

interface BuildingSettingsModalCommonProps {
  open: boolean;
  initialValues: BuildingFormValues;
  // The parent site context is required for create — buildings always
  // live under a site in the UI. Passed through so the modal can echo
  // the site label in the header copy.
  parentSiteLabel?: string;
  onDismiss: () => void;
  saving?: boolean;
}

// Discriminated union mirrors SiteSettingsModal. Edit gets onSave +
// onDeleteRequested; create gets onSave only (Delete is meaningless
// before the row exists).
export type BuildingSettingsModalProps = BuildingSettingsModalCommonProps &
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

// Layout-dimension cap. Matches the buf.validate int32.lte on
// Create/UpdateBuildingRequest in proto/buildings/v1/buildings.proto.
// 100 × 100 = 10,000 cells stays responsive in the ManageBuildingModal
// grid; anything above that risks a browser hang on render.
const LAYOUT_DIMENSION_MAX = 100;

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

const parseNonNegativeInt = (input: string): number | null => {
  const trimmed = input.trim();
  if (trimmed === "") return 0;
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0 || !Number.isInteger(parsed)) return null;
  return parsed;
};

const BuildingSettingsModal = (props: BuildingSettingsModalProps) => {
  const { open, initialValues, parentSiteLabel, onDismiss, saving = false } = props;
  const [name, setName] = useState(initialValues.name);
  const [powerText, setPowerText] = useState(
    initialValues.powerCapacityMw > 0 ? String(initialValues.powerCapacityMw) : "",
  );
  const [overheadText, setOverheadText] = useState(
    initialValues.overheadKw > 0 ? String(initialValues.overheadKw) : "",
  );
  const [aislesText, setAislesText] = useState(initialValues.aisles > 0 ? String(initialValues.aisles) : "");
  const [racksPerAisleText, setRacksPerAisleText] = useState(
    initialValues.racksPerAisle > 0 ? String(initialValues.racksPerAisle) : "",
  );
  const [powerError, setPowerError] = useState<string | null>(null);
  const [overheadError, setOverheadError] = useState<string | null>(null);
  const [aislesError, setAislesError] = useState<string | null>(null);
  const [racksPerAisleError, setRacksPerAisleError] = useState<string | null>(null);

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

    const aisles = parseNonNegativeInt(aislesText);
    if (aisles === null) {
      setAislesError("Whole number ≥ 0");
      return null;
    }
    if (aisles > LAYOUT_DIMENSION_MAX) {
      setAislesError(`Must be ≤ ${LAYOUT_DIMENSION_MAX}`);
      return null;
    }
    setAislesError(null);

    const racksPerAisle = parseNonNegativeInt(racksPerAisleText);
    if (racksPerAisle === null) {
      setRacksPerAisleError("Whole number ≥ 0");
      return null;
    }
    if (racksPerAisle > LAYOUT_DIMENSION_MAX) {
      setRacksPerAisleError(`Must be ≤ ${LAYOUT_DIMENSION_MAX}`);
      return null;
    }
    setRacksPerAisleError(null);

    return {
      name: name.trim(),
      // description + the rack-default block are deferred fields not
      // exposed in this form — preserve the server snapshot so an edit
      // here doesn't clobber values another caller wrote.
      description: initialValues.description,
      powerCapacityMw: power,
      overheadKw: overhead,
      aisles,
      racksPerAisle,
      physicalRackCount: initialValues.physicalRackCount,
      defaultRackRows: initialValues.defaultRackRows,
      defaultRackColumns: initialValues.defaultRackColumns,
      defaultRackOrderIndex: initialValues.defaultRackOrderIndex,
    };
  }, [
    name,
    powerText,
    overheadText,
    aislesText,
    racksPerAisleText,
    initialValues.description,
    initialValues.physicalRackCount,
    initialValues.defaultRackRows,
    initialValues.defaultRackColumns,
    initialValues.defaultRackOrderIndex,
  ]);

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
            testId: "building-settings-modal-cancel",
          },
          {
            text: saving ? "Saving…" : "Save",
            variant: variants.primary,
            onClick: handlePrimary,
            disabled: primaryDisabled,
            dismissModalOnClick: false,
            testId: "building-settings-modal-save",
          },
        ]
      : [
          {
            text: "Delete",
            variant: variants.secondaryDanger,
            onClick: props.onDeleteRequested,
            disabled: saving,
            testId: "building-settings-modal-delete",
          },
          {
            text: saving ? "Saving…" : "Save",
            variant: variants.primary,
            onClick: handlePrimary,
            disabled: primaryDisabled,
            dismissModalOnClick: false,
            testId: "building-settings-modal-save",
          },
        ];

  const title = "Building settings";
  const description = parentSiteLabel ? `in ${parentSiteLabel}` : undefined;

  return (
    <Modal
      open={open}
      onDismiss={saving ? undefined : onDismiss}
      title={title}
      description={description}
      buttons={buttons}
      testId="building-settings-modal"
    >
      <div className="flex flex-col gap-4 py-2">
        <Input
          id="building-settings-name"
          label="Name"
          initValue={name}
          onChange={(v) => setName(v)}
          maxLength={255}
          required
          autoFocus
          testId="building-settings-name-input"
        />
        <Select
          id="building-settings-type"
          label="Type"
          options={BUILDING_TYPE_OPTIONS}
          value=""
          onChange={() => undefined}
          disabled
          forceBelow
          testId="building-settings-type-select"
        />
        <Input
          id="building-settings-power"
          label="Power capacity"
          initValue={powerText}
          onChange={(v) => {
            setPowerText(v);
            if (powerError) setPowerError(null);
          }}
          units="MW"
          error={powerError ?? false}
          testId="building-settings-power-input"
        />
        <Input
          id="building-settings-overhead"
          label="Overhead"
          initValue={overheadText}
          onChange={(v) => {
            setOverheadText(v);
            if (overheadError) setOverheadError(null);
          }}
          units="kW"
          error={overheadError ?? false}
          testId="building-settings-overhead-input"
        />
        {/* Aisles / racks per aisle define the building's floor plan grid in
            ManageBuildingModal. Living in the settings modal lets the operator
            shape the layout up-front before assigning racks, mirroring how
            RackSettingsModal owns rows/columns for the rack grid. */}
        <div className="grid grid-cols-2 gap-4">
          <Input
            id="building-settings-aisles"
            label="Aisles"
            type="number"
            initValue={aislesText}
            onChange={(v) => {
              setAislesText(v);
              if (aislesError) setAislesError(null);
            }}
            error={aislesError ?? false}
            testId="building-settings-aisles-input"
          />
          <Input
            id="building-settings-racks-per-aisle"
            label="Racks per aisle"
            type="number"
            initValue={racksPerAisleText}
            onChange={(v) => {
              setRacksPerAisleText(v);
              if (racksPerAisleError) setRacksPerAisleError(null);
            }}
            error={racksPerAisleError ?? false}
            testId="building-settings-racks-per-aisle-input"
          />
        </div>
      </div>
    </Modal>
  );
};

export default BuildingSettingsModal;
