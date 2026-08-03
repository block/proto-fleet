import { type RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import {
  type DeviceSet,
  PerRackCreateErrorReason,
  RackCoolingType,
  RackOrderIndex,
  type RackType,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { type BulkCreateRackError, type NewRackInput, useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import {
  buildBulkRackLabels,
  bulkRackCountMaximum,
  overlongRackLabelIndexes,
  rackLabelMaxLength,
} from "@/protoFleet/features/fleetManagement/components/bulkRackLabels";
import { type RackFormData } from "@/protoFleet/features/fleetManagement/components/ManageRackModal/types";
import {
  counterScaleMaximum,
  counterScaleMinimum,
  counterStartInputMaxLength,
  defaultCounterScale,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/RenameOptionsModals/constants";
import { useHasPermission } from "@/protoFleet/store";

import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Select, { type SelectOption } from "@/shared/components/Select";

export type { RackFormData };

// Where a batch of racks lands. One placement for the whole batch, because
// CreateRacks carries it per request rather than per row.
export interface BulkRackPlacement {
  siteId?: bigint;
  buildingId?: bigint;
}

interface RackSettingsModalProps {
  show: boolean;
  existingRacks: DeviceSet[];
  initialFormData?: RackFormData;
  // Prepopulates the Site dropdown when creating a rack with no prior
  // placement (e.g. the page-header site scope). Ignored when
  // initialFormData already carries a siteId.
  defaultSiteId?: bigint;
  // Locks the placement to one building, for a create launched from inside that
  // building (its rack picker). Id and label travel together because the
  // building options are fetched per site — and a site-less building never
  // appears in that fetch at all — so the locked select can only render a label
  // the host hands it.
  //
  // defaultSiteId is NOT required alongside it. A building may sit in no site,
  // and the rack inherits whichever site the building has — see siteLocked.
  defaultBuilding?: { id: bigint; label: string };
  // True when editing an existing rack (which has a real, possibly-NULL
  // placement). Seeds the placement selects to "Unassigned" when the rack is
  // unplaced (vs. the empty placeholder on create) — see isExistingRack.
  existingRack?: boolean;
  onDismiss: () => void;
  // May be async: the caller persists the settings (an UpdateDeviceSet for an
  // existing rack, a create for a new one), so we await it and keep the button
  // busy until it resolves — a rejection leaves the modal open for a retry.
  onSubmit?: (formData: RackFormData) => void | Promise<unknown>;
  // Supplying onSubmitBulk is what turns on the Single / Multiple toggle. Entry
  // points with no batch handler wired render exactly as before — better than a
  // toggle whose CTA has nothing to call. Ignored when editing: a batch create
  // has no meaning for a rack that already exists.
  //
  // Resolves to the server's per-row rejections: an empty array means every rack
  // was created (the batch is all-or-nothing, so there is no partial case) and
  // the host closes the modal. A non-empty array leaves the modal open with the
  // offending preview rows marked.
  onSubmitBulk?: (racks: NewRackInput[], placement: BulkRackPlacement) => Promise<BulkCreateRackError[]>;
  // Which side of the Single / Multiple toggle the modal opens on, so a host with
  // two create entry points ("Create rack" / "Create multiple racks") lands the
  // operator on the form they asked for. Ignored without onSubmitBulk.
  initialCreateVariant?: CreateVariant;
  // Caller-driven busy state, OR'd with our own in-flight submit. Needed for
  // writes the caller retries on its own — a create that came back with a
  // reparent conflict is re-dispatched from the confirmation dialog, long after
  // our awaited onSubmit resolved.
  saving?: boolean;
}

// Create-mode variants. "single" is the original one-rack form; "multiple"
// swaps the Label field for generated labels + a read-only preview. Everything
// else on the form (placement, zone, geometry) applies to the whole batch.
type CreateVariant = "single" | "multiple";

const createVariantSegments = [
  { key: "single", title: "Single" },
  { key: "multiple", title: "Multiple" },
];

// Digits only, so a pasted "1e3" or "-4" can't reach the counter math.
const digitsOnly = (input: string, maxLength: number): string => input.replace(/\D/g, "").slice(0, maxLength);

const bulkCountInputMaxLength = String(bulkRackCountMaximum).length;

const bulkRowErrorMessage = (reason: PerRackCreateErrorReason): string => {
  switch (reason) {
    // Not "at this building": rack labels are unique per organization, so the
    // rack this one collides with may be in a different site entirely.
    case PerRackCreateErrorReason.DUPLICATE_LABEL_IN_ORG:
      return "Already used by another rack";
    case PerRackCreateErrorReason.DUPLICATE_LABEL_IN_BATCH:
      return "Repeated in this batch";
    default:
      return "Rejected";
  }
};

// Explicit "Unassigned" entry for the placement dropdowns. The shared Select
// has no clear affordance, so without this a user who picks a site/building
// could never revert to unassigned. A sentinel value (not "") is used so that
// an empty string still renders as the unselected placeholder — "Unassigned"
// only shows once the operator deliberately picks it or an existing rack seeds
// it. On submit the sentinel maps to undefined, same as an empty selection.
const UNASSIGNED_VALUE = "unassigned";
const UNASSIGNED_OPTION: SelectOption = { value: UNASSIGNED_VALUE, label: "Unassigned" };
// A real site/building id is selected (not the placeholder or the Unassigned
// sentinel) — gates building fetch, the building select's enabled state, and
// how the value is encoded onto RackFormData.
const isRealId = (value: string): boolean => value !== "" && value !== UNASSIGNED_VALUE;

const orderIndexOptions: SelectOption[] = [
  { value: String(RackOrderIndex.BOTTOM_LEFT), label: "Bottom left" },
  { value: String(RackOrderIndex.TOP_LEFT), label: "Top left" },
  { value: String(RackOrderIndex.BOTTOM_RIGHT), label: "Bottom right" },
  { value: String(RackOrderIndex.TOP_RIGHT), label: "Top right" },
];

const coolingTypeOptions: SelectOption[] = [
  { value: String(RackCoolingType.AIR), label: "Air" },
  { value: String(RackCoolingType.IMMERSION), label: "Immersion" },
];

const RackSettingsModal = ({
  show,
  existingRacks,
  initialFormData,
  defaultSiteId,
  defaultBuilding,
  existingRack,
  onDismiss,
  onSubmit,
  onSubmitBulk,
  initialCreateVariant,
  saving,
}: RackSettingsModalProps) => {
  const { listRackZones, listRackTypes } = useDeviceSets();
  const { sites } = useSitesContext();
  const { listBuildingsBySite } = useBuildings();
  // Placing a rack under a site/building is a site:manage action (the server
  // enforces the same on SaveRack). A rack:manage-only operator can still edit
  // rack contents, so the placement selects are hidden and no placement change
  // is submitted (ManageRackModal omits it).
  const canManagePlacement = useHasPermission("site:manage");

  // An already-persisted rack has a real placement (a site/building or NULL),
  // so an unplaced rack seeds the explicit "Unassigned" value. Creating a rack
  // treats placement as an optional, unfilled field: the default is the empty
  // placeholder (reads as "not chosen"), though "Unassigned" is still pickable
  // so a chosen site/building can be reverted.
  const isExistingRack = !!existingRack;

  // Creating from inside a building: the rack is being added to THAT building,
  // so lock the field rather than letting the operator create it somewhere the
  // host's list would never show it.
  const buildingLocked = !isExistingRack && canManagePlacement && defaultBuilding !== undefined;

  // Locked either by a page-header site scope (defaultSiteId is only set for a
  // single-site scope) or by a locked building, which owns the site: the rack
  // lands in that building, so its site is whatever the building's is —
  // including none. Locking both together is what keeps the operator from
  // picking a site that contradicts the building. An unscoped create leaves
  // Site editable/optional; edit is never locked.
  const siteLocked = !isExistingRack && canManagePlacement && (defaultSiteId !== undefined || buildingLocked);

  // Placement. Site is retained even when a building is chosen (it's the
  // building's site) so downstream eligibility filtering can pin the site;
  // saveRack drops it from the wire RackInfo.
  const [siteIdText, setSiteIdText] = useState<string>(() => {
    if (initialFormData?.siteId !== undefined) return initialFormData.siteId.toString();
    // Create + page-header scope: prefill the site the field is locked to. A
    // locked building with no site reads "Unassigned" — truthful, and it maps to
    // no site_id on submit so the server derives one from the building.
    if (siteLocked) return defaultSiteId?.toString() ?? UNASSIGNED_VALUE;
    // Edit of an unplaced rack shows "Unassigned"; unscoped create shows the
    // empty placeholder.
    return isExistingRack ? UNASSIGNED_VALUE : "";
  });
  const [buildingIdText, setBuildingIdText] = useState<string>(() => {
    if (initialFormData?.buildingId !== undefined) return initialFormData.buildingId.toString();
    if (buildingLocked) return defaultBuilding.id.toString();
    return isExistingRack ? UNASSIGNED_VALUE : "";
  });
  const [buildings, setBuildings] = useState<BuildingWithCounts[]>([]);

  const [label, setLabel] = useState(initialFormData?.label ?? "");
  const [zone, setZone] = useState(() => {
    // Editing an existing rack: its stored zone is authoritative, INCLUDING an
    // intentional "" — a blank zone is now a valid state. Use presence, not
    // truthiness, and never fall through to the last-rack default, which would
    // resurrect a just-cleared zone and re-persist it on save.
    if (isExistingRack) return initialFormData?.zone ?? "";
    // Create: seed from the form if it carries a zone, otherwise default to the
    // most recently created rack's zone as a convenience.
    if (initialFormData?.zone) return initialFormData.zone;
    if (existingRacks.length > 0) {
      const sorted = [...existingRacks].sort((a, b) => {
        const aTime = a.createdAt?.seconds ?? BigInt(0);
        const bTime = b.createdAt?.seconds ?? BigInt(0);
        return aTime > bTime ? -1 : aTime < bTime ? 1 : 0;
      });
      const lastZone = sorted[0].typeDetails.case === "rackInfo" ? sorted[0].typeDetails.value.zone : undefined;
      if (lastZone) return lastZone;
    }
    return "";
  });
  const initRows = initialFormData?.rows;
  const initCols = initialFormData?.columns;
  const [rackTypeSelection, setRackTypeSelection] = useState(initCols && initRows ? `${initCols}x${initRows}` : "new");
  const [rows, setRows] = useState(initRows ? String(initRows) : "");
  const [columns, setColumns] = useState(initCols ? String(initCols) : "");
  const [orderIndex, setOrderIndex] = useState<RackOrderIndex>(
    initialFormData?.orderIndex ?? RackOrderIndex.BOTTOM_LEFT,
  );
  const [coolingType, setCoolingType] = useState<RackCoolingType>(initialFormData?.coolingType ?? RackCoolingType.AIR);
  const [ownSubmit, setOwnSubmit] = useState(false);
  const isSubmitting = ownSubmit || !!saving;
  const [labelError, setLabelError] = useState<string | undefined>();
  const [columnsError, setColumnsError] = useState<string | undefined>();
  const [rowsError, setRowsError] = useState<string | undefined>();

  // Bulk (Multiple) create. Only reachable on a create, where onSubmitBulk is
  // wired: an existing rack has one label to edit, not a series to generate.
  const bulkAvailable = onSubmitBulk !== undefined && !isExistingRack;
  const [createVariant, setCreateVariant] = useState<CreateVariant>(
    (bulkAvailable ? initialCreateVariant : undefined) ?? "single",
  );
  const isBulk = bulkAvailable && createVariant === "multiple";
  const [bulkCountText, setBulkCountText] = useState("");
  const [bulkPrefix, setBulkPrefix] = useState("");
  const [bulkCounterStartText, setBulkCounterStartText] = useState("1");
  const [bulkCounterScaleText, setBulkCounterScaleText] = useState(String(defaultCounterScale));
  const [bulkCountError, setBulkCountError] = useState<string | undefined>();
  const [bulkPrefixError, setBulkPrefixError] = useState<string | undefined>();
  const [bulkCounterScaleError, setBulkCounterScaleError] = useState<string | undefined>();
  // Per-row rejections from the last attempt. Indexes point into the payload
  // that was submitted, so any edit to the bulk fields clears them rather than
  // leaving marks against rows they no longer describe.
  const [bulkRowErrors, setBulkRowErrors] = useState<BulkCreateRackError[]>([]);

  // Returns the same array when there is nothing to clear, so typing in a bulk
  // field doesn't re-render the (up to 500-row) preview on every keystroke.
  const clearBulkRowErrors = useCallback(() => {
    setBulkRowErrors((prev) => (prev.length === 0 ? prev : []));
  }, []);

  const [zoneSuggestions, setZoneSuggestions] = useState<string[]>([]);
  const [rackTypes, setRackTypes] = useState<RackType[]>([]);
  const [showZoneSuggestions, setShowZoneSuggestions] = useState(false);
  const [zonesLoaded, setZonesLoaded] = useState(false);
  const [rackTypesLoaded, setRackTypesLoaded] = useState(false);
  const isInitialLoading = !zonesLoaded || !rackTypesLoaded;
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const zoneInputRef = useRef<HTMLInputElement>(null) as RefObject<HTMLInputElement>;

  // Fetch data on mount
  useEffect(() => {
    listRackZones({
      onSuccess: (zones) => {
        setZoneSuggestions(zones);
        setHighlightedIndex(-1);
      },
      onFinally: () => setZonesLoaded(true),
    });
    listRackTypes({
      onSuccess: (types) => {
        setRackTypes(types);
        if (!initialFormData && types.length > 0) {
          const first = types[0];
          setRackTypeSelection(`${first.columns}x${first.rows}`);
          setRows(String(first.rows));
          setColumns(String(first.columns));
        }
      },
      onFinally: () => setRackTypesLoaded(true),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only run on mount; initialFormData is an initial value
  }, [listRackZones, listRackTypes]);

  // Load the selected site's buildings so the Building dropdown can scope its
  // options. Runs on mount (edit: shows the rack's current building) and on
  // every site change. Aborts in-flight fetches so a fast site switch can't
  // land stale options. With no real site selected there's nothing to fetch.
  useEffect(() => {
    if (!isRealId(siteIdText)) return;
    const controller = new AbortController();
    listBuildingsBySite({
      siteId: BigInt(siteIdText),
      signal: controller.signal,
      onSuccess: setBuildings,
    });
    return () => controller.abort();
  }, [siteIdText, listBuildingsBySite]);

  // Zone is sub-building, so it belongs to the rack's original building. When
  // the form moves to a different building the zone is cleared; returning to
  // the original building before saving restores it. Mirrors the server's
  // clear-on-building-change so the field shows what will actually persist.
  const originalBuildingText = initialFormData?.buildingId !== undefined ? initialFormData.buildingId.toString() : "";
  const originalZone = initialFormData?.zone ?? "";
  const reconcileZoneForBuilding = useCallback(
    (nextBuildingText: string) => {
      // Only an existing rack has a persisted building to cross. On create the
      // server stores whatever zone is submitted, so leave the typed/seeded
      // zone alone as the operator picks a building.
      if (!isExistingRack) return;
      const selected = isRealId(nextBuildingText) ? nextBuildingText : "";
      setZone(selected !== "" && selected === originalBuildingText ? originalZone : "");
    },
    [isExistingRack, originalBuildingText, originalZone],
  );

  // Changing the site clears the building selection (the old building lives in
  // a different site) and drops its now-stale options until the new site's
  // buildings load. The building — and therefore the zone — resets too. The
  // shared Select fires onChange on every option click, so no-op when the same
  // site is reselected — otherwise a confirm of the current site would clear
  // the building/zone and re-encode the rack as direct-under-site on save.
  const handleSiteChange = useCallback(
    (value: string) => {
      if (value === siteIdText) return;
      setSiteIdText(value);
      setBuildingIdText("");
      setBuildings([]);
      reconcileZoneForBuilding("");
    },
    [siteIdText, reconcileZoneForBuilding],
  );

  const handleBuildingChange = useCallback(
    (value: string) => {
      // Same no-op guard as the site select: reselecting the current building
      // must not reset the zone.
      if (value === buildingIdText) return;
      setBuildingIdText(value);
      reconcileZoneForBuilding(value);
    },
    [buildingIdText, reconcileZoneForBuilding],
  );

  const siteSelected = isRealId(siteIdText);

  const siteOptions = useMemo<SelectOption[]>(() => {
    const real = (sites ?? [])
      .filter((s) => s.site !== undefined)
      .map((s) => ({ value: s.site!.id.toString(), label: s.site!.name }))
      .sort((a, b) => a.label.localeCompare(b.label));
    // Unassigned is always offered so a chosen site can be reverted; the empty
    // placeholder (no option with value "") remains the unselected default.
    return [UNASSIGNED_OPTION, ...real];
  }, [sites]);

  const buildingOptions = useMemo<SelectOption[]>(() => {
    // A locked building is the only option, rendered from the host's label so
    // the field reads correctly before (and regardless of) the building fetch —
    // the id and label travel together, so it can't come out blank.
    if (buildingLocked) {
      return [{ value: defaultBuilding.id.toString(), label: defaultBuilding.label }];
    }
    if (!siteSelected) return [UNASSIGNED_OPTION];
    const real = buildings
      .filter((b) => b.building !== undefined)
      .map((b) => ({ value: b.building!.id.toString(), label: b.building!.name }))
      .sort((a, b) => a.label.localeCompare(b.label));
    return [UNASSIGNED_OPTION, ...real];
  }, [siteSelected, buildings, buildingLocked, defaultBuilding]);

  // The exact labels the batch will submit. The preview renders this same
  // array, so what the operator reads is what CreateRacks receives.
  const bulkLabels = useMemo(
    () =>
      buildBulkRackLabels(Number(bulkCountText || "0"), {
        namePrefix: bulkPrefix,
        counterStart: Number(bulkCounterStartText || "1"),
        counterScale: Number(bulkCounterScaleText),
      }),
    [bulkCountText, bulkPrefix, bulkCounterStartText, bulkCounterScaleText],
  );

  const bulkServerRows = useMemo(() => new Map(bulkRowErrors.map((e) => [e.index, e.reason])), [bulkRowErrors]);

  // Rows too long for the server to store. Derived from the labels rather than a
  // submit attempt, so the marks appear as the prefix/counter are edited — the
  // whole batch is refused on one overlong row, and which rows those are is not
  // obvious when the padding is what pushed them over.
  const bulkOverlongRows = useMemo(() => new Set(overlongRackLabelIndexes(bulkLabels)), [bulkLabels]);

  const filteredSuggestions = useMemo(() => {
    if (!zone.trim()) return zoneSuggestions;
    const lower = zone.toLowerCase();
    return zoneSuggestions.filter((s) => s.toLowerCase().includes(lower));
  }, [zone, zoneSuggestions]);

  const selectSuggestion = useCallback((suggestion: string) => {
    setZone(suggestion);
    setShowZoneSuggestions(false);
    setHighlightedIndex(-1);
    zoneInputRef.current?.blur();
  }, []);

  // Use refs for values needed in the native keydown handler to avoid stale closures
  const suggestionsStateRef = useRef({ showZoneSuggestions, filteredSuggestions, highlightedIndex });
  useEffect(() => {
    suggestionsStateRef.current = { showZoneSuggestions, filteredSuggestions, highlightedIndex };
  }, [showZoneSuggestions, filteredSuggestions, highlightedIndex]);
  const mouseInPopoverRef = useRef(false);

  // Attach native keydown to prevent default for arrow keys and Enter when navigating suggestions
  useEffect(() => {
    const input = zoneInputRef.current;
    if (!input) return;

    const handler = (e: KeyboardEvent) => {
      const {
        showZoneSuggestions: show,
        filteredSuggestions: suggestions,
        highlightedIndex: idx,
      } = suggestionsStateRef.current;
      if (!show || suggestions.length === 0 || mouseInPopoverRef.current) return;

      if (e.key === "ArrowDown") {
        e.preventDefault();
        setHighlightedIndex((prev) => (prev < suggestions.length - 1 ? prev + 1 : prev));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setHighlightedIndex((prev) => (prev > 0 ? prev - 1 : -1));
      } else if (e.key === "Enter" && idx >= 0) {
        e.preventDefault();
        selectSuggestion(suggestions[idx]);
      }
    };

    input.addEventListener("keydown", handler);
    return () => input.removeEventListener("keydown", handler);
  }, [selectSuggestion]);

  const rackTypeDisabled = rackTypeSelection !== "new";

  const rackTypeOptions: SelectOption[] = useMemo(() => {
    const opts: SelectOption[] = rackTypes.map((rt) => ({
      value: `${rt.columns}x${rt.rows}`,
      label: `${rt.columns}x${rt.rows} (${rt.rackCount} ${rt.rackCount === 1 ? "rack" : "racks"})`,
    }));
    opts.push({ value: "new", label: "New Layout" });
    return opts;
  }, [rackTypes]);

  const handleRackTypeChange = useCallback(
    (value: string) => {
      setRackTypeSelection(value);
      if (value === "new") {
        setRows("");
        setColumns("");
      } else {
        const rt = rackTypes.find((t) => `${t.columns}x${t.rows}` === value);
        if (rt) {
          setRows(String(rt.rows));
          setColumns(String(rt.columns));
        }
      }
    },
    [rackTypes],
  );

  // Exactly the fields handleSubmit puts on RackFormData, compared against what
  // the form was seeded with — so this reads clean only when the write would be
  // a no-op. Creating a rack is always a real write, so it's never clean.
  // Placement is included even when the selects are hidden
  // (rack:manage-only): they're then seeded from initialFormData and can't
  // diverge, so this reads clean either way.
  const isDirty = useMemo(() => {
    if (!isExistingRack || !initialFormData) return true;
    return (
      label.trim() !== initialFormData.label ||
      zone.trim() !== initialFormData.zone ||
      Number(rows) !== initialFormData.rows ||
      Number(columns) !== initialFormData.columns ||
      orderIndex !== initialFormData.orderIndex ||
      coolingType !== initialFormData.coolingType ||
      (isRealId(siteIdText) ? BigInt(siteIdText) : undefined) !== initialFormData.siteId ||
      (isRealId(buildingIdText) ? BigInt(buildingIdText) : undefined) !== initialFormData.buildingId
    );
  }, [
    isExistingRack,
    initialFormData,
    label,
    zone,
    rows,
    columns,
    orderIndex,
    coolingType,
    siteIdText,
    buildingIdText,
  ]);

  const handleSubmit = useCallback(async () => {
    setLabelError(undefined);
    setColumnsError(undefined);
    setRowsError(undefined);
    setBulkCountError(undefined);
    setBulkPrefixError(undefined);
    setBulkCounterScaleError(undefined);

    let hasError = false;

    // Multiple generates the labels, so the Label field isn't on screen to
    // validate; the prefix and row count take its place.
    if (isBulk) {
      if (bulkPrefix.trim() === "") {
        setBulkPrefixError("A label prefix is required");
        hasError = true;
      }
      if (bulkLabels.length === 0) {
        setBulkCountError("Enter how many racks to create");
        hasError = true;
      } else if (Number(bulkCountText) > bulkRackCountMaximum) {
        // buildBulkRackLabels clamps to the cap, so a larger typed count would
        // silently create only the first bulkRackCountMaximum racks.
        setBulkCountError(`Create up to ${bulkRackCountMaximum} racks at a time`);
        hasError = true;
      }
      // Reported on the prefix because that is the lever: the counter width is
      // set by the scale and the start value, both of which the operator chose on
      // purpose. Can't collide with the missing-prefix message above — bare
      // counters are never this long.
      if (bulkOverlongRows.size > 0) {
        setBulkPrefixError(`Labels must be ${rackLabelMaxLength} characters or fewer, counter included`);
        hasError = true;
      }
      // Empty scale is the default (no padding), so only a typed value is
      // bounded. digitsOnly + maxLength 1 means it can only ever be 0–9.
      const scale = bulkCounterScaleText === "" ? defaultCounterScale : Number(bulkCounterScaleText);
      if (scale < counterScaleMinimum || scale > counterScaleMaximum) {
        setBulkCounterScaleError(`Must be ${counterScaleMinimum}–${counterScaleMaximum}`);
        hasError = true;
      }
    } else if (!label.trim()) {
      setLabelError("A label is required");
      hasError = true;
    }
    const colsNum = Number(columns);
    if (!Number.isInteger(colsNum) || colsNum < 1 || colsNum > 12) {
      setColumnsError("Columns must be a whole number between 1 and 12");
      hasError = true;
    }
    const rowsNum = Number(rows);
    if (!Number.isInteger(rowsNum) || rowsNum < 1 || rowsNum > 12) {
      setRowsError("Rows must be a whole number between 1 and 12");
      hasError = true;
    }

    if (hasError) return;

    // Placeholder ("") and the Unassigned sentinel both encode as undefined.
    const siteId = isRealId(siteIdText) ? BigInt(siteIdText) : undefined;
    const buildingId = isRealId(buildingIdText) ? BigInt(buildingIdText) : undefined;

    if (isBulk && onSubmitBulk) {
      setOwnSubmit(true);
      try {
        // Clear first so marks from the previous attempt don't hang over rows
        // this one may have fixed.
        setBulkRowErrors([]);
        // `?? []` because a host that resolves nothing means "no rejections" —
        // storing undefined would take the preview down with it.
        setBulkRowErrors(
          (await onSubmitBulk(
            bulkLabels.map((bulkLabel) => ({
              label: bulkLabel,
              rows: rowsNum,
              columns: colsNum,
              zone: zone.trim(),
              orderIndex,
              coolingType,
            })),
            { siteId, buildingId },
          )) ?? [],
        );
      } finally {
        setOwnSubmit(false);
      }
      return;
    }

    // Valid but unchanged. An existing rack's Save would re-persist identical
    // values and toast as though something changed, so close instead. Not an
    // error state — keeping what's already there is a legitimate outcome.
    if (!isDirty) {
      onDismiss?.();
      return;
    }

    const formData: RackFormData = {
      label: label.trim(),
      zone: zone.trim(),
      rows: rowsNum,
      columns: colsNum,
      orderIndex,
      coolingType,
      siteId,
      buildingId,
    };

    // The caller owns the write — an UpdateDeviceSet for an existing rack, a
    // create for a new one. Await it and keep the button busy so a slow save
    // can't be double-submitted; the caller leaves this modal open on failure
    // so the operator can retry.
    setOwnSubmit(true);
    try {
      await onSubmit?.(formData);
    } finally {
      setOwnSubmit(false);
    }
  }, [
    label,
    zone,
    rows,
    columns,
    orderIndex,
    coolingType,
    siteIdText,
    buildingIdText,
    onSubmit,
    isDirty,
    onDismiss,
    isBulk,
    onSubmitBulk,
    bulkLabels,
    bulkCountText,
    bulkOverlongRows,
    bulkPrefix,
    bulkCounterScaleText,
  ]);

  if (!show) return null;

  // What a preview row is marked with, if anything. Length comes first: it says
  // why the batch can't be sent at all, where a server reason describes an
  // attempt that has already been made.
  const bulkRowError = (index: number): string | undefined => {
    if (bulkOverlongRows.has(index)) return `Over ${rackLabelMaxLength} characters`;
    const reason = bulkServerRows.get(index);
    return reason === undefined ? undefined : bulkRowErrorMessage(reason);
  };

  // Named for the write the button makes: creating one rack, creating a batch,
  // or saving settings onto a rack that already exists.
  const submitText = (): string => {
    if (isExistingRack) return isSubmitting ? "Saving..." : "Save";
    if (isSubmitting) return "Creating...";
    return isBulk ? "Create racks" : "Create rack";
  };

  return (
    <Modal
      open={show}
      title="Rack settings"
      phoneSheet
      // Block dismiss (X / backdrop) while a settings save is in flight — the
      // write persists regardless, so closing mid-request would be a surprise.
      // Re-enabled once it resolves (or fails, leaving the form open).
      onDismiss={isSubmitting ? () => {} : onDismiss}
      divider={false}
      buttons={[
        {
          text: submitText(),
          variant: "primary",
          // Only the in-flight and not-yet-loaded guards: validation and the
          // no-diff check run in handleSubmit, where they can say what's wrong. A
          // form diffed against an unfetched baseline can't produce a correct
          // write, hence isInitialLoading.
          disabled: isSubmitting || isInitialLoading,
          loading: isSubmitting,
          onClick: handleSubmit,
          dismissModalOnClick: false,
        },
      ]}
    >
      {isInitialLoading ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <div className="flex flex-col gap-4 pt-1">
          {bulkAvailable ? (
            <SegmentedControl
              segments={createVariantSegments}
              initialSegmentKey={createVariant}
              onSelect={(key) => {
                setCreateVariant(key === "multiple" ? "multiple" : "single");
                // Each mode keeps its own fields, so switching never rewrites
                // the other side's input — but marks from a batch attempt
                // describe a payload that is no longer on screen.
                clearBulkRowErrors();
              }}
            />
          ) : null}

          {isBulk ? (
            // Row count sits above the label properties: it decides how many
            // rows the preview has, while the properties below decide what each
            // one is called.
            <Input
              id="rack-bulk-count"
              label="Number of racks"
              type="number"
              initValue={bulkCountText}
              maxLength={bulkCountInputMaxLength}
              onChange={(value) => {
                setBulkCountText(digitsOnly(value, bulkCountInputMaxLength));
                clearBulkRowErrors();
                if (bulkCountError) setBulkCountError(undefined);
              }}
              required
              error={bulkCountError}
              autoFocus
              testId="rack-bulk-count-input"
            />
          ) : (
            <Input
              id="rack-label"
              label="Label"
              initValue={label}
              maxLength={rackLabelMaxLength}
              onChange={(value) => {
                setLabel(value);
                if (labelError) setLabelError(undefined);
              }}
              error={labelError}
            />
          )}

          {isBulk ? (
            // Labelled the way the bulk-rename counter fields are (see
            // CustomPropertyOptionsModal) and the bulk building form — same
            // prefix + start + scale triple, so the vocabulary carries over.
            <fieldset className="rounded-xl border border-border-5 p-4">
              <legend className="px-1 text-emphasis-300 text-text-primary">Bulk properties</legend>
              <div className="grid grid-cols-1 gap-4 tablet:grid-cols-3">
                <Input
                  id="rack-bulk-prefix"
                  label="Label prefix"
                  initValue={bulkPrefix}
                  // The label cap, not the cap minus a guessed counter width: how
                  // wide the counter runs depends on the scale and start value, so
                  // the overflow is reported per row (and on submit) instead.
                  maxLength={rackLabelMaxLength}
                  onChange={(value) => {
                    setBulkPrefix(value);
                    clearBulkRowErrors();
                    if (bulkPrefixError) setBulkPrefixError(undefined);
                  }}
                  required
                  error={bulkPrefixError}
                  testId="rack-bulk-prefix-input"
                />
                <Input
                  id="rack-bulk-counter-start"
                  label="Counter start (optional)"
                  type="number"
                  initValue={bulkCounterStartText}
                  maxLength={counterStartInputMaxLength}
                  onChange={(value) => {
                    setBulkCounterStartText(digitsOnly(value, counterStartInputMaxLength));
                    clearBulkRowErrors();
                  }}
                  testId="rack-bulk-counter-start-input"
                />
                {/* A number input rather than bulk rename's radio row: it's a
                    digit count sitting next to another number field, and the
                    range is enforced on submit like any other bound. */}
                <Input
                  id="rack-bulk-counter-scale"
                  label="Counter scale (optional)"
                  type="number"
                  initValue={bulkCounterScaleText}
                  maxLength={1}
                  onChange={(value) => {
                    setBulkCounterScaleText(digitsOnly(value, 1));
                    clearBulkRowErrors();
                    if (bulkCounterScaleError) setBulkCounterScaleError(undefined);
                  }}
                  error={bulkCounterScaleError}
                  testId="rack-bulk-counter-scale-input"
                />
              </div>
            </fieldset>
          ) : null}

          {canManagePlacement ? (
            <>
              <Select
                id="rack-site-select"
                label={siteLocked ? "Site" : "Site (optional)"}
                options={siteOptions}
                value={siteIdText}
                onChange={handleSiteChange}
                disabled={siteLocked}
                forceBelow
                testId="rack-site-select"
              />

              <Select
                id="rack-building-select"
                label={buildingLocked ? "Building" : "Building (optional)"}
                options={buildingOptions}
                value={buildingIdText}
                onChange={handleBuildingChange}
                // A building can't be chosen without a real site — it scopes the
                // options and supplies the derived site_id.
                disabled={buildingLocked || !siteSelected}
                forceBelow
                testId="rack-building-select"
              />
            </>
          ) : null}

          <div className="relative">
            <Input
              id="rack-zone"
              label="Zone (optional)"
              initValue={zone}
              // RackInfo.zone shares the label's buf.validate max_len, and it is
              // free text with no other guard.
              maxLength={rackLabelMaxLength}
              inputRef={zoneInputRef}
              onChange={(value) => {
                setZone(value);
                setHighlightedIndex(-1);
              }}
              onFocus={() => setShowZoneSuggestions(true)}
              onBlur={() => {
                if (!mouseInPopoverRef.current) {
                  setShowZoneSuggestions(false);
                }
              }}
              autoComplete="off"
            />
            {showZoneSuggestions && filteredSuggestions.length > 0 ? (
              <div
                className="absolute top-full z-10 mt-1 w-full rounded-xl border border-border-5 bg-surface-elevated-base p-1.5 shadow-300"
                onMouseEnter={() => {
                  mouseInPopoverRef.current = true;
                  setHighlightedIndex(-1);
                }}
                onMouseLeave={() => {
                  mouseInPopoverRef.current = false;
                }}
              >
                {filteredSuggestions.map((suggestion, index) => (
                  <button
                    key={suggestion}
                    type="button"
                    className={clsx(
                      "w-full rounded-xl px-3 py-2.5 text-left text-300 text-text-primary",
                      { "bg-core-primary-5": index === highlightedIndex },
                      "hover:bg-core-primary-5",
                    )}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => selectSuggestion(suggestion)}
                  >
                    {suggestion}
                  </button>
                ))}
              </div>
            ) : null}
          </div>

          {rackTypes.length > 0 ? (
            <Select
              id="rack-type-select"
              label="Rack type"
              options={rackTypeOptions}
              value={rackTypeSelection}
              onChange={handleRackTypeChange}
              testId="rack-type-select"
            />
          ) : null}

          <div className="grid grid-cols-2 gap-3 tablet:grid-cols-3">
            <div className="flex-1">
              <Input
                id="rack-columns"
                label="Columns"
                type="number"
                initValue={columns}
                onChange={(value) => {
                  setColumns(value);
                  if (columnsError) setColumnsError(undefined);
                }}
                disabled={rackTypeDisabled}
                error={columnsError}
              />
            </div>
            <div className="flex-1">
              <Input
                id="rack-rows"
                label="Rows"
                type="number"
                initValue={rows}
                onChange={(value) => {
                  setRows(value);
                  if (rowsError) setRowsError(undefined);
                }}
                disabled={rackTypeDisabled}
                error={rowsError}
              />
            </div>
            <Select
              id="order-index-select"
              label="Order index"
              options={orderIndexOptions}
              value={String(orderIndex)}
              onChange={(v) => setOrderIndex(Number(v) as RackOrderIndex)}
              testId="order-index-select"
              className="flex-1"
            />
          </div>

          <Select
            id="cooling-type-select"
            label="Cooling type"
            options={coolingTypeOptions}
            value={String(coolingType)}
            onChange={(v) => setCoolingType(Number(v) as RackCoolingType)}
            testId="cooling-type-select"
          />

          {isBulk ? (
            // Preview, not a form: these are exactly the labels the batch
            // submits, so there is nothing to edit here — an operator who wants
            // different labels changes the properties above.
            <section className="flex flex-col gap-2" data-testid="rack-bulk-preview">
              <span className="text-emphasis-300 text-text-primary">
                {bulkLabels.length} {bulkLabels.length === 1 ? "rack" : "racks"} to create
              </span>
              {bulkLabels.length === 0 ? (
                <p className="rounded-xl border border-dashed border-border-5 p-4 text-center text-300 text-text-primary-50">
                  Enter a number of racks and a label prefix to preview them
                </p>
              ) : (
                // No scroll of its own: the modal body already scrolls, and a
                // nested scroller would trap the wheel over the longest part of
                // the form.
                <ol className="divide-y divide-border-5 rounded-xl border border-border-5">
                  {bulkLabels.map((bulkLabel, index) => {
                    const rowError = bulkRowError(index);
                    return (
                      <li
                        key={`${bulkLabel}-${index}`}
                        className="flex items-center gap-3 px-3 py-2"
                        data-testid={`rack-bulk-preview-row-${index}`}
                      >
                        <span className="w-8 shrink-0 text-300 text-text-primary-50">{index + 1}</span>
                        <span className="min-w-0 flex-1 truncate text-300 text-text-primary">{bulkLabel}</span>
                        {rowError !== undefined ? (
                          // Same token Input renders its validation text in, so a
                          // row error reads like a field error.
                          <span className="shrink-0 text-200 text-intent-critical-fill">{rowError}</span>
                        ) : null}
                      </li>
                    );
                  })}
                </ol>
              )}
            </section>
          ) : null}
        </div>
      )}
    </Modal>
  );
};

export default RackSettingsModal;
