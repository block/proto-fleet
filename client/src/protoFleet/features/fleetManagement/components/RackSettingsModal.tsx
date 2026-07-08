import { type RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import {
  type DeviceSet,
  RackCoolingType,
  RackOrderIndex,
  type RackType,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { type RackFormData } from "@/protoFleet/features/fleetManagement/components/ManageRackModal/types";

import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select, { type SelectOption } from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export type { RackFormData };

interface RackSettingsModalProps {
  show: boolean;
  existingRacks: DeviceSet[];
  rack?: DeviceSet;
  initialFormData?: RackFormData;
  // Prepopulates the Site dropdown when creating a rack with no prior
  // placement (e.g. the page-header site scope). Ignored when
  // initialFormData already carries a siteId.
  defaultSiteId?: bigint;
  // True when editing an existing rack (which has a real, possibly-NULL
  // placement). Drives the "Unassigned" placement option — see
  // showUnassignedOption. The embedded modal inside ManageRackModal can't tell
  // create from edit on its own (it always runs in onContinue mode), so the
  // caller passes it.
  existingRack?: boolean;
  onDismiss: () => void;
  onContinue?: (formData: RackFormData) => void;
  onSuccess?: () => void;
}

// Explicit "Unassigned" entry for the placement dropdowns. The shared Select
// has no clear affordance, so without this a user who picks a site/building
// could never revert to unassigned.
const UNASSIGNED_OPTION: SelectOption = { value: "", label: "Unassigned" };

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
  rack,
  initialFormData,
  defaultSiteId,
  existingRack,
  onDismiss,
  onContinue,
  onSuccess,
}: RackSettingsModalProps) => {
  const isEditMode = !!rack;
  const rackInfo = rack?.typeDetails.case === "rackInfo" ? rack.typeDetails.value : undefined;

  const { updateRack, listRackZones, listRackTypes } = useDeviceSets();
  const { sites } = useSitesContext();
  const { listBuildingsBySite } = useBuildings();

  // Editing an already-persisted rack surfaces an explicit "Unassigned" option
  // seeded to the rack's real placement (a rack genuinely has a site/building
  // or NULL). Creating a rack treats placement as an optional, unfilled field
  // — no "Unassigned" entry, empty by default — so an empty select reads as
  // "not chosen" (→ NULL) rather than a deliberate-looking default.
  const showUnassignedOption = existingRack || isEditMode;

  // Creating within a page-header site scope: the rack belongs to that site,
  // so lock the field to it (defaultSiteId is only set for a single-site
  // scope). An unscoped create leaves Site editable/optional; edit is never
  // locked.
  const siteLocked = !showUnassignedOption && defaultSiteId !== undefined;

  // Placement. Site is retained even when a building is chosen (it's the
  // building's site) so downstream eligibility filtering can pin the site;
  // saveRack drops it from the wire RackInfo. Empty string = Unassigned.
  const [siteIdText, setSiteIdText] = useState<string>(() => {
    if (initialFormData?.siteId !== undefined) return initialFormData.siteId.toString();
    // Create-only prefill — edit seeds solely from the rack's real placement,
    // so an unplaced rack reads as Unassigned rather than the page scope.
    if (!showUnassignedOption && defaultSiteId !== undefined) return defaultSiteId.toString();
    return "";
  });
  const [buildingIdText, setBuildingIdText] = useState<string>(
    initialFormData?.buildingId !== undefined ? initialFormData.buildingId.toString() : "",
  );
  const [buildings, setBuildings] = useState<BuildingWithCounts[]>([]);

  const [label, setLabel] = useState(initialFormData?.label ?? rack?.label ?? "");
  const [zone, setZone] = useState(() => {
    if (initialFormData?.zone) return initialFormData.zone;
    if (rackInfo?.zone) return rackInfo.zone;
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
  const initRows = initialFormData?.rows ?? rackInfo?.rows;
  const initCols = initialFormData?.columns ?? rackInfo?.columns;
  const [rackTypeSelection, setRackTypeSelection] = useState(initCols && initRows ? `${initCols}x${initRows}` : "new");
  const [rows, setRows] = useState(initRows ? String(initRows) : "");
  const [columns, setColumns] = useState(initCols ? String(initCols) : "");
  const [orderIndex, setOrderIndex] = useState<RackOrderIndex>(
    initialFormData?.orderIndex ?? rackInfo?.orderIndex ?? RackOrderIndex.BOTTOM_LEFT,
  );
  const [coolingType, setCoolingType] = useState<RackCoolingType>(
    initialFormData?.coolingType ?? rackInfo?.coolingType ?? RackCoolingType.AIR,
  );
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const [labelError, setLabelError] = useState<string | undefined>();
  const [columnsError, setColumnsError] = useState<string | undefined>();
  const [rowsError, setRowsError] = useState<string | undefined>();

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
        if (!initialFormData && !rackInfo && types.length > 0) {
          const first = types[0];
          setRackTypeSelection(`${first.columns}x${first.rows}`);
          setRows(String(first.rows));
          setColumns(String(first.columns));
        }
      },
      onFinally: () => setRackTypesLoaded(true),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only run on mount; initialFormData and rackInfo are initial values
  }, [listRackZones, listRackTypes]);

  // Load the selected site's buildings so the Building dropdown can scope its
  // options. Runs on mount (edit: shows the rack's current building) and on
  // every site change. Aborts in-flight fetches so a fast site switch can't
  // land stale options. When no site is selected there's nothing to fetch —
  // buildingOptions falls back to Unassigned-only.
  useEffect(() => {
    if (siteIdText === "") return;
    const controller = new AbortController();
    listBuildingsBySite({
      siteId: BigInt(siteIdText),
      signal: controller.signal,
      onSuccess: setBuildings,
    });
    return () => controller.abort();
  }, [siteIdText, listBuildingsBySite]);

  // Changing the site clears the building selection (the old building lives in
  // a different site) and drops its now-stale options until the new site's
  // buildings load.
  const handleSiteChange = useCallback((value: string) => {
    setSiteIdText(value);
    setBuildingIdText("");
    setBuildings([]);
  }, []);

  const siteOptions = useMemo<SelectOption[]>(() => {
    const real = (sites ?? [])
      .filter((s) => s.site !== undefined)
      .map((s) => ({ value: s.site!.id.toString(), label: s.site!.name }))
      .sort((a, b) => a.label.localeCompare(b.label));
    return showUnassignedOption ? [UNASSIGNED_OPTION, ...real] : real;
  }, [sites, showUnassignedOption]);

  const buildingOptions = useMemo<SelectOption[]>(() => {
    // No site selected → nothing to scope to. Edit still offers Unassigned so
    // the current NULL building reads clearly; create shows an empty
    // placeholder.
    if (siteIdText === "") return showUnassignedOption ? [UNASSIGNED_OPTION] : [];
    const real = buildings
      .filter((b) => b.building !== undefined)
      .map((b) => ({ value: b.building!.id.toString(), label: b.building!.name }))
      .sort((a, b) => a.label.localeCompare(b.label));
    return showUnassignedOption ? [UNASSIGNED_OPTION, ...real] : real;
  }, [siteIdText, buildings, showUnassignedOption]);

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

  const handleSubmit = useCallback(() => {
    setLabelError(undefined);
    setColumnsError(undefined);
    setRowsError(undefined);
    setErrorMsg("");

    let hasError = false;

    if (!label.trim()) {
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

    const formData: RackFormData = {
      label: label.trim(),
      zone: zone.trim(),
      rows: rowsNum,
      columns: colsNum,
      orderIndex,
      coolingType,
      siteId: siteIdText !== "" ? BigInt(siteIdText) : undefined,
      buildingId: buildingIdText !== "" ? BigInt(buildingIdText) : undefined,
    };

    if (!isEditMode) {
      onContinue?.(formData);
      return;
    }

    setIsSubmitting(true);

    updateRack({
      deviceSetId: rack!.id,
      label: formData.label,
      zone: formData.zone,
      rows: formData.rows,
      columns: formData.columns,
      orderIndex: formData.orderIndex,
      coolingType: formData.coolingType,
      onSuccess: () => {
        pushToast({
          message: `Rack "${formData.label}" updated`,
          status: STATUSES.success,
        });
        onSuccess?.();
        onDismiss();
      },
      onError: (error) => {
        setErrorMsg(error || "Failed to update rack. Please try again.");
      },
      onFinally: () => {
        setIsSubmitting(false);
      },
    });
  }, [
    label,
    zone,
    rows,
    columns,
    orderIndex,
    coolingType,
    siteIdText,
    buildingIdText,
    isEditMode,
    rack,
    updateRack,
    onContinue,
    onSuccess,
    onDismiss,
  ]);

  if (!show) return null;

  return (
    <Modal
      open={show}
      title="Rack settings"
      phoneSheet
      onDismiss={onDismiss}
      divider={false}
      buttons={[
        {
          text: isEditMode ? (isSubmitting ? "Saving..." : "Save") : "Continue",
          variant: "primary",
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
          {errorMsg ? <Callout intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

          <Input
            id="rack-label"
            label="Label"
            initValue={label}
            onChange={(value) => setLabel(value)}
            error={labelError}
          />

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
            label="Building (optional)"
            options={buildingOptions}
            value={buildingIdText}
            onChange={setBuildingIdText}
            // A building can't be chosen without a site — it scopes the
            // options and supplies the derived site_id.
            disabled={siteIdText === ""}
            forceBelow
            testId="rack-building-select"
          />

          <div className="relative">
            <Input
              id="rack-zone"
              label="Zone (optional)"
              initValue={zone}
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
                onChange={(value) => setColumns(value)}
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
                onChange={(value) => setRows(value)}
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
        </div>
      )}
    </Modal>
  );
};

export default RackSettingsModal;
