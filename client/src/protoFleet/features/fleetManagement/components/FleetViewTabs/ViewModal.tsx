import { type ReactNode, useCallback, useMemo, useState } from "react";
import clsx from "clsx";
import type { FleetTabId } from "@/protoFleet/features/fleetManagement/views/savedViews";
import {
  diffDisplaySummaries,
  diffFilterSummaries,
  diffSortSummaries,
  type DisplayDiff,
  type DisplaySummary,
  type FilterDiff,
  type FilterDiffEntry,
  type FilterSummaryEntry,
  type SortDiff,
  type SortSummary,
} from "@/protoFleet/features/fleetManagement/views/viewSummary";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Switch from "@/shared/components/Switch";

export type ViewModalMode =
  | { kind: "create"; tab: FleetTabId }
  | {
      kind: "update";
      /**
       * `update` replaces the saved filter/sort/display set with the current
       * URL state. `rename` is a name-only edit and must NOT touch the saved
       * params — even when the active view is dirty.
       */
      intent: "update" | "rename";
      viewId: string;
      tab: FleetTabId;
      currentName: string;
      savedFilters: FilterSummaryEntry[];
      savedSort: SortSummary | undefined;
      savedDisplay: DisplaySummary | undefined;
    };

export type ViewModalState =
  | { open: false }
  | {
      open: true;
      mode: ViewModalMode;
      defaultName: string;
      currentFilters: FilterSummaryEntry[];
      currentSort: SortSummary | undefined;
      currentDisplay: DisplaySummary | undefined;
    };

type ViewModalProps = {
  state: ViewModalState;
  existingNames: string[];
  onDismiss: () => void;
  onSubmit: (input: { name: string; includeSort: boolean; includeDisplay: boolean }) => void;
};

const CHANGE_LABELS: Record<FilterDiffEntry["change"], string | undefined> = {
  unchanged: undefined,
  added: "Added",
  changed: "Changed",
};

const CHANGE_BADGE_CLASS: Record<FilterDiffEntry["change"], string> = {
  unchanged: "",
  added: "bg-intent-success-10 text-intent-success-fill",
  changed: "bg-intent-warning-10 text-intent-warning-fill",
};

const ChangeBadge = ({ change }: { change: FilterDiffEntry["change"] }) => {
  const text = CHANGE_LABELS[change];
  if (!text) return null;
  return (
    <span
      className={clsx("rounded-md px-1.5 py-0.5 text-200", CHANGE_BADGE_CLASS[change])}
      data-testid={`view-summary-change-${change}`}
    >
      {text}
    </span>
  );
};

const FilterSummaryRows = ({ entries, diff }: { entries: FilterSummaryEntry[]; diff: FilterDiff | undefined }) => {
  if (!diff) {
    if (entries.length === 0) return <EmptyState />;
    return (
      <ul className="flex flex-col" data-testid="view-summary-list">
        {entries.map((entry) => (
          <li key={entry.key} className="flex items-baseline gap-2 text-300" data-testid={`view-summary-${entry.key}`}>
            <span className="text-text-primary-70">{entry.label}:</span>
            <span className="text-text-primary">{entry.values.join(", ")}</span>
          </li>
        ))}
      </ul>
    );
  }

  if (diff.current.length === 0 && diff.removed.length === 0) return <EmptyState />;

  return (
    <ul className="flex flex-col" data-testid="view-summary-list">
      {diff.current.map((entry) => (
        <li key={entry.key} className="flex items-baseline gap-2 text-300" data-testid={`view-summary-${entry.key}`}>
          <span className="text-text-primary-70">{entry.label}:</span>
          <span className="text-text-primary">{entry.values.join(", ")}</span>
          <ChangeBadge change={entry.change} />
        </li>
      ))}
      {diff.removed.map((entry) => (
        <li
          key={`removed-${entry.key}`}
          className="flex items-baseline gap-2 text-300"
          data-testid={`view-summary-removed-${entry.key}`}
        >
          <span className="text-text-primary-70">{entry.label}:</span>
          <span className="text-text-primary">{entry.values.join(", ")}</span>
          <span
            className="rounded-md bg-intent-critical-10 px-1.5 py-0.5 text-200 text-intent-critical-fill"
            data-testid="view-summary-change-removed"
          >
            Removed
          </span>
        </li>
      ))}
    </ul>
  );
};

const EmptyState = () => (
  <div className="rounded-lg bg-surface-5 px-4 py-3 text-300 text-text-primary-70" data-testid="view-summary-empty">
    No filters applied. Saving will create an unfiltered view.
  </div>
);

const sortDescription = (sort: SortSummary | undefined) =>
  sort ? `${sort.fieldLabel} (${sort.direction === "asc" ? "ascending" : "descending"})` : "No sort applied";

const SortSection = ({
  current,
  diff,
  includeSort,
  setIncludeSort,
}: {
  current: SortSummary | undefined;
  diff: SortDiff | undefined;
  includeSort: boolean;
  setIncludeSort: (checked: boolean | ((prev: boolean) => boolean)) => void;
}) => {
  const renderBadge = (): ReactNode => {
    switch (diff?.change) {
      case "added":
      case "changed":
        return <ChangeBadge change={diff.change} />;
      case "removed":
        return (
          <span
            className="rounded-md bg-intent-critical-10 px-1.5 py-0.5 text-200 text-intent-critical-fill"
            data-testid="view-summary-sort-change-removed"
          >
            Removed
          </span>
        );
      default:
        return null;
    }
  };

  return (
    <div className="flex items-center justify-between gap-4" data-testid="view-summary-include-sort">
      <div className="flex flex-col">
        <span className="text-emphasis-300 text-text-primary">Include sort order</span>
        <span className="inline-flex items-center gap-2 text-300 text-text-primary-70">
          <span>{sortDescription(current)}</span>
          {renderBadge()}
        </span>
      </div>
      <Switch checked={includeSort} setChecked={setIncludeSort} disabled={!current} />
    </div>
  );
};

const displayDescription = (display: DisplaySummary | undefined) => display?.label ?? "No display mode set";

const DisplaySection = ({
  current,
  diff,
  includeDisplay,
  setIncludeDisplay,
}: {
  current: DisplaySummary | undefined;
  diff: DisplayDiff | undefined;
  includeDisplay: boolean;
  setIncludeDisplay: (checked: boolean | ((prev: boolean) => boolean)) => void;
}) => {
  const renderBadge = (): ReactNode => {
    switch (diff?.change) {
      case "added":
      case "changed":
        return <ChangeBadge change={diff.change} />;
      case "removed":
        return (
          <span
            className="rounded-md bg-intent-critical-10 px-1.5 py-0.5 text-200 text-intent-critical-fill"
            data-testid="view-summary-display-change-removed"
          >
            Removed
          </span>
        );
      default:
        return null;
    }
  };

  return (
    <div className="flex items-center justify-between gap-4" data-testid="view-summary-include-display">
      <div className="flex flex-col">
        <span className="text-emphasis-300 text-text-primary">Include display mode</span>
        <span className="inline-flex items-center gap-2 text-300 text-text-primary-70">
          <span>{displayDescription(current)}</span>
          {renderBadge()}
        </span>
      </div>
      <Switch checked={includeDisplay} setChecked={setIncludeDisplay} disabled={!current} />
    </div>
  );
};

const ViewModal = ({ state, existingNames, onDismiss, onSubmit }: ViewModalProps) => {
  const open = state.open;
  const defaultName = state.open ? state.defaultName : "";

  const [name, setName] = useState(defaultName);
  const [error, setError] = useState<string | undefined>(undefined);
  const [includeSort, setIncludeSort] = useState(true);
  const [includeDisplay, setIncludeDisplay] = useState(true);

  const [prevOpen, setPrevOpen] = useState(open);
  if (prevOpen !== open) {
    setPrevOpen(open);
    setName(defaultName);
    setError(undefined);
    setIncludeSort(true);
    setIncludeDisplay(true);
  }

  const filterDiff = useMemo<FilterDiff | undefined>(() => {
    if (!state.open || state.mode.kind !== "update") return undefined;
    return diffFilterSummaries(state.currentFilters, state.mode.savedFilters);
  }, [state]);

  const sortDiff = useMemo<SortDiff | undefined>(() => {
    if (!state.open || state.mode.kind !== "update") return undefined;
    return diffSortSummaries(state.currentSort, state.mode.savedSort);
  }, [state]);

  const displayDiff = useMemo<DisplayDiff | undefined>(() => {
    if (!state.open || state.mode.kind !== "update") return undefined;
    return diffDisplaySummaries(state.currentDisplay, state.mode.savedDisplay);
  }, [state]);

  const handleSubmit = useCallback(() => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Name is required");
      return;
    }
    const conflict = existingNames.some((existing) => existing.toLowerCase() === trimmed.toLowerCase());
    if (conflict) {
      setError("A view with this name already exists");
      return;
    }
    onSubmit({ name: trimmed, includeSort, includeDisplay });
  }, [name, existingNames, onSubmit, includeSort, includeDisplay]);

  const intent = state.open && state.mode.kind === "update" ? state.mode.intent : undefined;
  const isRename = intent === "rename";
  const isUpdate = intent === "update";
  const title = isRename ? "Rename view" : isUpdate ? "Update view" : "New view";
  const description = isRename
    ? "Update the view's name. Saved filters and sort are left unchanged."
    : isUpdate
      ? "Replace the saved filters and sort with the current set."
      : "Save the current filters and sort as a view.";
  const submitText = isRename ? "Rename" : isUpdate ? "Update" : "Save";

  // Only surface the display toggle on tabs that publish a `display` URL
  // value (racks today, sites once it lands). If `currentDisplay` is
  // undefined the section would just disable the switch and read "No
  // display mode set" — that's noise on miners/buildings, so hide it.
  const currentDisplay = state.open ? state.currentDisplay : undefined;
  const savedDisplay = state.open && state.mode.kind === "update" ? state.mode.savedDisplay : undefined;
  const showDisplaySection = currentDisplay !== undefined || savedDisplay !== undefined;

  return (
    <Modal
      open={open}
      title={title}
      description={description}
      onDismiss={onDismiss}
      testId="view-modal"
      buttons={[
        { text: "Cancel", onClick: onDismiss, variant: variants.secondary },
        { text: submitText, onClick: handleSubmit, variant: variants.primary, dismissModalOnClick: false },
      ]}
    >
      <div className="flex flex-col gap-6">
        <Input
          id="view-name"
          label="Name"
          initValue={defaultName}
          autoFocus
          error={error ?? false}
          onChange={(value) => {
            setName(value);
            setError(undefined);
          }}
          onKeyDown={(key) => {
            if (key === "Enter") handleSubmit();
          }}
        />

        {!isRename ? (
          <>
            <div className="flex flex-col">
              <span className="text-emphasis-300 text-text-primary">Filters</span>
              <FilterSummaryRows entries={state.open ? state.currentFilters : []} diff={filterDiff} />
            </div>

            <SortSection
              current={state.open ? state.currentSort : undefined}
              diff={sortDiff}
              includeSort={includeSort}
              setIncludeSort={setIncludeSort}
            />

            {showDisplaySection ? (
              <DisplaySection
                current={currentDisplay}
                diff={displayDiff}
                includeDisplay={includeDisplay}
                setIncludeDisplay={setIncludeDisplay}
              />
            ) : null}
          </>
        ) : null}
      </div>
    </Modal>
  );
};

export default ViewModal;
