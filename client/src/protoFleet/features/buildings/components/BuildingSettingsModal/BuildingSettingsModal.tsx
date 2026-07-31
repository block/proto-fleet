import { useCallback, useMemo, useState } from "react";

import {
  buildBulkBuildingNames,
  buildingNameMaxLength,
  bulkBuildingCountMaximum,
  overlongBuildingNameIndexes,
  takenNameIndexes,
} from "./bulkBuildingNames";
import {
  type BuildingFormValues,
  type BulkCreateBuildingError,
  type NewBuildingInput,
} from "@/protoFleet/api/buildings";
import { PerBuildingCreateErrorReason } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import {
  counterScaleMaximum,
  counterScaleMinimum,
  counterStartInputMaxLength,
  defaultCounterScale,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/RenameOptionsModals/constants";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";
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

// Create mode owns site selection: when initialSiteId is supplied the
// dropdown locks to that site (entry from /sites/:id, a site-scoped row, or a
// page-header site scope — the building belongs to that site); otherwise it's
// editable and Save stays disabled until a site is chosen.
interface BuildingSettingsModalCreateExtras {
  sites: SiteWithCounts[];
  initialSiteId?: bigint;
  // Supplying onSaveBulk is what turns on the Single / Multiple toggle. Create
  // entry points that have no batch handler wired render exactly as before —
  // better than showing a toggle whose CTA has nothing to call.
  //
  // Resolves to the server's per-row rejections: an empty array means every
  // building was created (the batch is all-or-nothing, so there is no partial
  // case) and the host closes the modal. A non-empty array leaves the modal
  // open with the offending preview rows marked.
  onSaveBulk?: (buildings: NewBuildingInput[], siteId: bigint) => Promise<BulkCreateBuildingError[]>;
  // Which side of the Single / Multiple toggle the modal opens on. Lets a host
  // with two create entry points ("Create building" / "Create multiple
  // buildings") land the operator on the form they asked for. Ignored without
  // onSaveBulk — there's no toggle to preselect.
  initialCreateVariant?: CreateVariant;
  // Names of the buildings already at the target site, used to mark a colliding
  // preview row before the request goes out. Only meaningful when the site is
  // locked — with an editable dropdown the host can't know which site's names
  // to pass, so the collision surfaces from the server instead.
  existingBuildingNames?: string[];
}

// Discriminated union mirrors SiteSettingsModal. Edit gets onSave +
// onDeleteRequested; create gets onSave (with the chosen siteId).
// Delete is meaningless before the row exists.
export type BuildingSettingsModalProps = BuildingSettingsModalCommonProps &
  (
    | ({
        mode: "create";
        onSave: (values: BuildingFormValues, siteId: bigint) => Promise<void> | void;
      } & BuildingSettingsModalCreateExtras)
    | {
        mode: "edit";
        onSave: (values: BuildingFormValues) => Promise<void> | void;
        onDeleteRequested: () => void;
      }
  );

// Building type is deliberately absent from this form. The comps show it, but
// the concept needs its own definition (and its own proto enum) before a
// dropdown here means anything — a permanently disabled stub only advertised a
// field nobody could use.

// Layout-dimension cap. Matches the buf.validate int32.lte on
// Create/UpdateBuildingRequest in proto/buildings/v1/buildings.proto.
// 100 × 100 = 10,000 cells stays responsive in the ManageBuildingModal
// grid; anything above that risks a browser hang on render.
const LAYOUT_DIMENSION_MAX = 100;

// Create-mode variants. "single" is the original one-building form; "multiple"
// swaps the per-building fields for generated names + a read-only preview.
type CreateVariant = "single" | "multiple";

const createVariantSegments = [
  { key: "single", title: "Single" },
  { key: "multiple", title: "Multiple" },
];

// Digits only, so a pasted "1e3" or "-4" can't reach the counter math.
const digitsOnly = (input: string, maxLength: number): string => input.replace(/\D/g, "").slice(0, maxLength);

const bulkCountInputMaxLength = String(bulkBuildingCountMaximum).length;

const bulkRowErrorMessage = (reason: PerBuildingCreateErrorReason): string => {
  switch (reason) {
    case PerBuildingCreateErrorReason.DUPLICATE_NAME_AT_SITE:
      return "Already used at this site";
    case PerBuildingCreateErrorReason.DUPLICATE_NAME_IN_BATCH:
      return "Repeated in this batch";
    default:
      return "Rejected";
  }
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
  const isCreate = props.mode === "create";
  // Lock the Site dropdown when create mode opens with a pre-filled site
  // (entry from /sites/:id or a site-scoped row). Buildings-tab CTA opens
  // with no initialSiteId, so the dropdown is editable.
  const initialSiteId = isCreate ? props.initialSiteId : undefined;
  const sites = isCreate ? props.sites : undefined;
  const siteLocked = isCreate && initialSiteId !== undefined;
  const [siteIdText, setSiteIdText] = useState<string>(initialSiteId !== undefined ? initialSiteId.toString() : "");
  const [name, setName] = useState(initialValues.name);
  const [aislesText, setAislesText] = useState(initialValues.aisles > 0 ? String(initialValues.aisles) : "");
  const [racksPerAisleText, setRacksPerAisleText] = useState(
    initialValues.racksPerAisle > 0 ? String(initialValues.racksPerAisle) : "",
  );
  const [aislesError, setAislesError] = useState<string | null>(null);
  const [racksPerAisleError, setRacksPerAisleError] = useState<string | null>(null);
  const [nameError, setNameError] = useState<string | null>(null);
  const [siteSubmitError, setSiteSubmitError] = useState<string | null>(null);
  const [bulkCountError, setBulkCountError] = useState<string | null>(null);
  const [bulkPrefixError, setBulkPrefixError] = useState<string | null>(null);
  const [bulkCounterScaleError, setBulkCounterScaleError] = useState<string | null>(null);
  // Problems with no single field to hang off — currently only a batch whose
  // names collide with the site's existing ones, where the offending rows are
  // already marked in the preview but a bare click would otherwise look inert.
  const [formError, setFormError] = useState<string | null>(null);

  const onSaveBulk = isCreate ? props.onSaveBulk : undefined;
  const existingBuildingNames = isCreate ? props.existingBuildingNames : undefined;
  const [createVariant, setCreateVariant] = useState<CreateVariant>(
    (isCreate ? props.initialCreateVariant : undefined) ?? "single",
  );
  const [bulkCountText, setBulkCountText] = useState("");
  const [bulkPrefix, setBulkPrefix] = useState("");
  const [bulkCounterStartText, setBulkCounterStartText] = useState("1");
  const [bulkCounterScaleText, setBulkCounterScaleText] = useState(String(defaultCounterScale));
  // Per-row rejections from the last attempt. Indexes point into the payload
  // that was submitted, so any edit to the bulk fields clears them rather than
  // leaving marks against rows they no longer describe.
  const [bulkRowErrors, setBulkRowErrors] = useState<BulkCreateBuildingError[]>([]);
  const isBulk = onSaveBulk !== undefined && createVariant === "multiple";

  // Returns the same array when there is nothing to clear, so typing in a bulk
  // field doesn't re-render the (up to 500-row) preview on every keystroke.
  const clearBulkRowErrors = useCallback(() => {
    setBulkRowErrors((prev) => (prev.length === 0 ? prev : []));
  }, []);

  // Floor-plan grid, shared by both modes: single create writes it onto the one
  // building, Multiple applies it to every row of the batch. Both fields are
  // checked every call rather than returning at the first failure, so two bad
  // dimensions surface together instead of one per click.
  const buildLayout = useCallback((): { aisles: number; racksPerAisle: number } | null => {
    const aisles = parseNonNegativeInt(aislesText);
    const racksPerAisle = parseNonNegativeInt(racksPerAisleText);
    const dimensionError = (parsed: number | null) =>
      parsed === null ? "Whole number ≥ 0" : parsed > LAYOUT_DIMENSION_MAX ? `Must be ≤ ${LAYOUT_DIMENSION_MAX}` : null;
    const aislesMessage = dimensionError(aisles);
    const racksMessage = dimensionError(racksPerAisle);
    setAislesError(aislesMessage);
    setRacksPerAisleError(racksMessage);
    if (aisles === null || racksPerAisle === null || aislesMessage || racksMessage) return null;
    return { aisles, racksPerAisle };
  }, [aislesText, racksPerAisleText]);

  const buildValues = useCallback((): BuildingFormValues | null => {
    // buildLayout first and unconditionally: it sets both dimension errors, and
    // short-circuiting on the name would hide them.
    const layout = buildLayout();
    const trimmedName = name.trim();
    setNameError(trimmedName === "" ? "Enter a building name" : null);
    if (!layout || trimmedName === "") return null;

    return {
      name: trimmedName,
      // description, capacity and the rack-default block are fields this form
      // doesn't expose — preserve the server snapshot so an edit here doesn't
      // clobber values another caller wrote.
      description: initialValues.description,
      powerCapacityMw: initialValues.powerCapacityMw,
      overheadKw: initialValues.overheadKw,
      aisles: layout.aisles,
      racksPerAisle: layout.racksPerAisle,
      physicalRackCount: initialValues.physicalRackCount,
      defaultRackRows: initialValues.defaultRackRows,
      defaultRackColumns: initialValues.defaultRackColumns,
      defaultRackOrderIndex: initialValues.defaultRackOrderIndex,
    };
  }, [buildLayout, name, initialValues]);

  const siteOptions = useMemo(
    () =>
      (sites ?? [])
        .filter((s) => s.site !== undefined)
        .map((s) => ({ value: s.site!.id.toString(), label: s.site!.name }))
        .sort((a, b) => a.label.localeCompare(b.label)),
    [sites],
  );

  // The exact names the batch will submit. The preview renders this same array,
  // so what the operator reads is what CreateBuildings receives.
  const bulkNames = useMemo(
    () =>
      buildBulkBuildingNames(Number(bulkCountText || "0"), {
        namePrefix: bulkPrefix,
        counterStart: Number(bulkCounterStartText || "1"),
        counterScale: Number(bulkCounterScaleText),
      }),
    [bulkCountText, bulkPrefix, bulkCounterStartText, bulkCounterScaleText],
  );

  // Rows whose name is already at the site. Blocks the CTA, so the operator
  // adjusts the prefix or start number instead of eating a rejected round trip.
  const bulkTakenRows = useMemo(
    () => new Set(takenNameIndexes(bulkNames, existingBuildingNames ?? [])),
    [bulkNames, existingBuildingNames],
  );

  // Rows too long for the server to store. Derived from the names rather than a
  // submit attempt, so the marks appear as the prefix/counter are edited — one
  // overlong row fails the whole batch.
  const bulkOverlongRows = useMemo(() => new Set(overlongBuildingNameIndexes(bulkNames)), [bulkNames]);

  const bulkServerRows = useMemo(() => new Map(bulkRowErrors.map((e) => [e.index, e.reason])), [bulkRowErrors]);

  // Resolve the chosen site, guarding against a stale `sites` snapshot — a click
  // that races a `sites` refresh must not slip a deleted siteId through.
  const resolveSiteId = useCallback((): bigint | null => {
    if (siteIdText === "" || !siteOptions.some((o) => o.value === siteIdText)) return null;
    try {
      return BigInt(siteIdText);
    } catch {
      return null;
    }
  }, [siteIdText, siteOptions]);

  const siteInOptions = siteIdText !== "" && siteOptions.some((o) => o.value === siteIdText);
  // Two different failures, two different messages: a site that vanished from
  // the list needs a reload, one that was never picked just needs picking. Only
  // read when resolveSiteId came back null, so a non-empty id here means stale.
  const siteMissingMessage =
    siteIdText !== "" ? "Selected site is no longer available. Refresh and try again." : "Select a site";
  // The stale case can go true without a click — a `sites` refresh that drops
  // the chosen id — so it surfaces on its own instead of waiting for submit.
  const siteError = (isCreate && siteIdText !== "" && !siteInOptions ? siteMissingMessage : siteSubmitError) ?? false;

  // Compared against the same normalized shape buildValues produces, so
  // trailing whitespace or a dimension retyped with a leading zero doesn't read
  // as an edit. buildValues sets the *Error states as a side effect, so this
  // parses independently rather than calling it.
  const isDirty = useMemo(
    () =>
      name.trim() !== initialValues.name ||
      parseNonNegativeInt(aislesText) !== initialValues.aisles ||
      parseNonNegativeInt(racksPerAisleText) !== initialValues.racksPerAisle,
    [name, aislesText, racksPerAisleText, initialValues],
  );

  // Every branch validates on click and reports what's wrong, rather than the
  // CTA being disabled until the form happens to be valid. Each check runs
  // unconditionally so one submit surfaces every problem at once.
  const handlePrimary = useCallback(async () => {
    if (onSaveBulk && createVariant === "multiple") {
      const siteId = resolveSiteId();
      const layout = buildLayout();
      const prefixMissing = bulkPrefix.trim() === "";
      const noRows = bulkNames.length === 0;
      const collides = bulkTakenRows.size > 0;
      // Reported on the prefix because that is the lever: the counter width is
      // set by the scale and start value, both chosen on purpose. Can't collide
      // with the missing-prefix message — bare counters are never this long.
      const overlong = bulkOverlongRows.size > 0;
      // Empty scale is the default (no padding), so only a typed value is
      // bounded. digitsOnly + maxLength 1 means it can only ever be 0–9.
      const scale = bulkCounterScaleText === "" ? defaultCounterScale : Number(bulkCounterScaleText);
      const scaleOutOfRange = scale < counterScaleMinimum || scale > counterScaleMaximum;
      setSiteSubmitError(siteId === null ? siteMissingMessage : null);
      setBulkPrefixError(
        prefixMissing
          ? "Enter a name prefix"
          : overlong
            ? `Names must be ${buildingNameMaxLength} characters or fewer, counter included`
            : null,
      );
      setBulkCountError(noRows ? "Enter how many buildings to create" : null);
      setBulkCounterScaleError(scaleOutOfRange ? `Must be ${counterScaleMinimum}–${counterScaleMaximum}` : null);
      setFormError(collides ? "Some of these names are already used at this site." : null);
      if (siteId === null || !layout || prefixMissing || noRows || collides || scaleOutOfRange || overlong) return;
      // Clear first so marks from the previous attempt don't hang over rows
      // this one may have fixed.
      setBulkRowErrors([]);
      setBulkRowErrors(
        await onSaveBulk(
          bulkNames.map((bulkName) => ({ name: bulkName, ...layout })),
          siteId,
        ),
      );
      return;
    }
    const values = buildValues();
    if (props.mode === "create") {
      const siteId = resolveSiteId();
      setSiteSubmitError(siteId === null ? siteMissingMessage : null);
      if (!values || siteId === null) return;
      await props.onSave(values, siteId);
      return;
    }
    if (!values) return;
    // Valid but unchanged: UpdateBuilding would report a write it didn't make.
    if (!isDirty) {
      onDismiss();
      return;
    }
    await props.onSave(values);
  }, [
    buildValues,
    buildLayout,
    props,
    createVariant,
    resolveSiteId,
    bulkNames,
    bulkOverlongRows,
    bulkPrefix,
    bulkCounterScaleText,
    bulkTakenRows,
    onSaveBulk,
    isDirty,
    onDismiss,
    siteMissingMessage,
  ]);

  // Only the in-flight guard. Everything the old gate checked — name, site,
  // layout, bulk prefix and row count, name collisions, and the edit-mode diff —
  // now runs in handlePrimary, where it can name the problem.
  const primaryDisabled = saving;

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
            // Named for the write it performs (CreateBuilding / CreateBuildings)
            // rather than a generic "Save" — the buildings exist once this lands.
            text: isBulk ? (saving ? "Creating…" : "Create buildings") : saving ? "Creating…" : "Create building",
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

  // Aisles / racks per aisle define the building's floor plan grid in
  // ManageBuildingModal. Living in the settings modal lets the operator shape
  // the layout up-front before assigning racks, mirroring how RackSettingsModal
  // owns rows/columns for the rack grid. Shared by both modes — in Multiple the
  // one value applies to every building in the batch.
  const layoutFields = (
    <div className="grid grid-cols-2 gap-4">
      <Input
        id="building-settings-aisles"
        label="Aisles (optional)"
        type="number"
        initValue={aislesText}
        onChange={(v) => {
          setAislesText(v);
          if (aislesError) setAislesError(null);
          clearBulkRowErrors();
        }}
        error={aislesError ?? false}
        testId="building-settings-aisles-input"
      />
      <Input
        id="building-settings-racks-per-aisle"
        label="Racks per aisle (optional)"
        type="number"
        initValue={racksPerAisleText}
        onChange={(v) => {
          setRacksPerAisleText(v);
          if (racksPerAisleError) setRacksPerAisleError(null);
          clearBulkRowErrors();
        }}
        error={racksPerAisleError ?? false}
        testId="building-settings-racks-per-aisle-input"
      />
    </div>
  );

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
        {/* Form-level slot, for a problem that belongs to the batch rather than
            to any one field. Same shape the alerts and settings modals use. */}
        {formError ? (
          <Callout
            intent="danger"
            prefixIcon={<Alert />}
            title={formError}
            testId="building-settings-modal-form-error"
          />
        ) : null}
        {onSaveBulk ? (
          <SegmentedControl
            segments={createVariantSegments}
            initialSegmentKey={createVariant}
            onSelect={(key) => {
              setCreateVariant(key === "multiple" ? "multiple" : "single");
              // Each mode keeps its own fields, so switching never rewrites the
              // other side's input — but marks from a batch attempt describe a
              // payload that is no longer on screen.
              clearBulkRowErrors();
            }}
          />
        ) : null}
        {isCreate ? (
          <Select
            id="building-settings-site"
            label="Site"
            options={siteOptions}
            value={siteIdText}
            onChange={(v) => {
              setSiteIdText(v);
              if (siteSubmitError) setSiteSubmitError(null);
            }}
            disabled={siteLocked}
            error={siteError}
            forceBelow
            testId="building-settings-site-select"
          />
        ) : null}
        {isBulk ? (
          <>
            {/* Row count lives above the bulk properties: it decides how many
                rows the preview has, while the properties below decide what
                each one is called. */}
            <Input
              id="building-settings-bulk-count"
              label="Number of buildings"
              type="number"
              initValue={bulkCountText}
              maxLength={bulkCountInputMaxLength}
              onChange={(v) => {
                setBulkCountText(digitsOnly(v, bulkCountInputMaxLength));
                clearBulkRowErrors();
                if (bulkCountError) setBulkCountError(null);
                if (formError) setFormError(null);
              }}
              required
              error={bulkCountError ?? false}
              autoFocus
              testId="building-settings-bulk-count-input"
            />
            {/* Three-up, and labelled the way the bulk-rename counter fields are
                (see CustomPropertyOptionsModal) — same prefix + start + scale
                triple, so the vocabulary carries over. "Counter start" drops
                bulk rename's trailing "number": a third of the modal's width
                can't hold that plus "(optional)" on one line. */}
            <fieldset className="rounded-xl border border-border-5 p-4">
              <legend className="px-1 text-emphasis-300 text-text-primary">Bulk properties</legend>
              <div className="grid grid-cols-1 gap-4 tablet:grid-cols-3">
                <Input
                  id="building-settings-bulk-prefix"
                  label="Name prefix"
                  initValue={bulkPrefix}
                  // The name cap, not the cap minus a guessed counter width: how
                  // wide the counter runs depends on the scale and start value, so
                  // the overflow is reported per row (and on submit) instead.
                  maxLength={buildingNameMaxLength}
                  onChange={(v) => {
                    setBulkPrefix(v);
                    clearBulkRowErrors();
                    if (bulkPrefixError) setBulkPrefixError(null);
                    if (formError) setFormError(null);
                  }}
                  required
                  error={bulkPrefixError ?? false}
                  testId="building-settings-bulk-prefix-input"
                />
                <Input
                  id="building-settings-bulk-counter-start"
                  label="Counter start (optional)"
                  type="number"
                  initValue={bulkCounterStartText}
                  maxLength={counterStartInputMaxLength}
                  onChange={(v) => {
                    setBulkCounterStartText(digitsOnly(v, counterStartInputMaxLength));
                    clearBulkRowErrors();
                    if (formError) setFormError(null);
                  }}
                  testId="building-settings-bulk-counter-start-input"
                />
                {/* A number input rather than bulk rename's radio row: it's a
                    digit count sitting next to another number field, and the
                    1–6 range is enforced on submit like any other bound. Single
                    digit by construction, so only 0 and 7–9 can be wrong. */}
                <Input
                  id="building-settings-bulk-counter-scale"
                  label="Counter scale (optional)"
                  type="number"
                  initValue={bulkCounterScaleText}
                  maxLength={1}
                  onChange={(v) => {
                    setBulkCounterScaleText(digitsOnly(v, 1));
                    clearBulkRowErrors();
                    if (bulkCounterScaleError) setBulkCounterScaleError(null);
                  }}
                  error={bulkCounterScaleError ?? false}
                  testId="building-settings-bulk-counter-scale-input"
                />
              </div>
            </fieldset>
            {/* One layout for the whole batch. Left optional: an operator who
                doesn't know the floor plan yet can create the buildings now and
                set dimensions per building later. */}
            <fieldset className="rounded-xl border border-border-5 p-4">
              <legend className="px-1 text-emphasis-300 text-text-primary">Layout</legend>
              {layoutFields}
            </fieldset>
            {/* Preview, not a form: these are exactly the names the batch
                submits, so there is nothing to edit here — an operator who
                wants different names changes the properties above. */}
            <section className="flex flex-col gap-2" data-testid="building-settings-bulk-preview">
              <span className="text-emphasis-300 text-text-primary">
                {bulkNames.length} {bulkNames.length === 1 ? "building" : "buildings"} to create
              </span>
              {bulkNames.length === 0 ? (
                <p className="rounded-xl border border-dashed border-border-5 p-4 text-center text-300 text-text-primary-50">
                  Enter a number of buildings and a name prefix to preview them
                </p>
              ) : (
                // No scroll of its own: the modal body already scrolls, and a
                // nested scroller would trap the wheel over the longest part of
                // the form.
                <ol className="divide-y divide-border-5 rounded-xl border border-border-5">
                  {bulkNames.map((bulkName, index) => {
                    // Length first: it says why the batch can't be sent at all,
                    // where the other two describe a name the server could store.
                    const serverReason = bulkServerRows.get(index);
                    const rowError = bulkOverlongRows.has(index)
                      ? `Over ${buildingNameMaxLength} characters`
                      : serverReason !== undefined
                        ? bulkRowErrorMessage(serverReason)
                        : bulkTakenRows.has(index)
                          ? "Already used at this site"
                          : null;
                    return (
                      <li
                        key={`${bulkName}-${index}`}
                        className="flex items-center gap-3 px-3 py-2"
                        data-testid={`building-settings-bulk-preview-row-${index}`}
                      >
                        <span className="w-8 shrink-0 text-300 text-text-primary-50">{index + 1}</span>
                        <span className="min-w-0 flex-1 truncate text-300 text-text-primary">{bulkName}</span>
                        {rowError ? (
                          // text-intent-critical-fill is the token Input renders
                          // its own validation text in, so a row error here
                          // reads the same as a field error. text-text-negative
                          // was a typo — no such token, so this rendered in the
                          // inherited color.
                          <span className="shrink-0 text-200 text-intent-critical-fill">{rowError}</span>
                        ) : null}
                      </li>
                    );
                  })}
                </ol>
              )}
            </section>
          </>
        ) : (
          <>
            <Input
              id="building-settings-name"
              label="Name"
              initValue={name}
              onChange={(v) => {
                setName(v);
                if (nameError) setNameError(null);
              }}
              maxLength={buildingNameMaxLength}
              required
              error={nameError ?? false}
              autoFocus
              testId="building-settings-name-input"
            />
            {layoutFields}
          </>
        )}
      </div>
    </Modal>
  );
};

export default BuildingSettingsModal;
