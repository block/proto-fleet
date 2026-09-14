import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import { Search as SearchIcon } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Search from "@/shared/components/Search";

const SEARCH_DEBOUNCE_MS = 250;

const MAX_SEARCH_QUERY_CODE_POINTS = 255;

// Leading whitespace never narrows a search, so it is dropped at the input,
// where `sanitize` applies it to the displayed text and the emitted value
// together. Trailing whitespace has to survive: stripping it would erase the
// space the moment it is typed and make multi-word queries impossible. Cap by
// Unicode code point rather than UTF-16 code unit so this matches the proto
// validators and the server's utf8.RuneCountInString checks.
const sanitizeSearchQuery = (value: string) =>
  Array.from(value.trimStart()).slice(0, MAX_SEARCH_QUERY_CODE_POINTS).join("");

export interface ListSearchInputProps {
  /** Accessible name for the field and, while collapsed, for the toggle. */
  label: string;
  id: string;
  initialValue?: string;
  onQueryChange: (query: string) => void;
  /** Fired synchronously on every keystroke, ahead of the debounce.
   *
   * Consumers that treat "a search is active" as a safety condition have to use
   * this rather than `onQueryChange`: for the debounce interval the applied
   * filter still reads as empty, so a selection gated on the applied filter
   * stays armed while the field already shows a query. */
  onQueryInput?: (query: string) => void;
  /** Collapses an empty field behind an icon button. Intended for list
   * toolbars; modal/picker search stays persistently visible by default. */
  collapsible?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
}

/** Search control for server-filtered lists. The visible input updates
 * immediately while requests are debounced so typing does not issue one RPC
 * per keystroke.
 *
 * The query is emitted exactly as typed. Callers persist it (to the URL or
 * local state) and feed it back as `initialValue`, and the input re-seeds
 * itself from that prop, so emitting a normalized form would overwrite the text
 * mid-entry — trimming here ate the space in "rack 7" whenever the debounce
 * fired between the two words. Trimming belongs at the consumers, which already
 * do it: every server search_query is trimmed before it is applied. */
const ListSearchInput = ({
  label,
  id,
  initialValue = "",
  onQueryChange,
  onQueryInput,
  collapsible = false,
  onExpandedChange,
}: ListSearchInputProps) => {
  const [expanded, setExpanded] = useState(!collapsible || Boolean(initialValue));
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // The pending timer must survive a new `onQueryChange` identity (the fleet
  // table rebuilds it on every navigation), so the callback is read from a ref
  // at fire time instead of being captured per keystroke.
  const onQueryChangeRef = useRef(onQueryChange);
  onQueryChangeRef.current = onQueryChange;
  const onQueryInputRef = useRef(onQueryInput);
  onQueryInputRef.current = onQueryInput;

  const handleChange = useCallback((value: string) => {
    onQueryInputRef.current?.(value);
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    if (!value) {
      timeoutRef.current = null;
      onQueryChangeRef.current("");
      return;
    }
    timeoutRef.current = setTimeout(() => {
      onQueryChangeRef.current(value);
    }, SEARCH_DEBOUNCE_MS);
  }, []);

  const previousInitialValueRef = useRef(initialValue);
  useEffect(() => {
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current);
      timeoutRef.current = null;
    }
    if (previousInitialValueRef.current !== initialValue) {
      previousInitialValueRef.current = initialValue;
      onQueryInputRef.current?.(initialValue);
    }
  }, [initialValue]);

  useEffect(
    () => () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
    },
    [],
  );

  // `expanded` is only the operator's toggle; a non-empty value expands the
  // field too. The parent has to hear about transitions of the combined state,
  // otherwise a query that arrives from navigation reports "expanded" and its
  // later removal collapses the field without ever reporting the collapse.
  const shouldExpand = expanded || Boolean(initialValue);
  const reportedExpandedRef = useRef(shouldExpand);
  const reportExpanded = useCallback(
    (nextExpanded: boolean) => {
      if (reportedExpandedRef.current === nextExpanded) return;
      reportedExpandedRef.current = nextExpanded;
      onExpandedChange?.(nextExpanded);
    },
    [onExpandedChange],
  );
  const setSearchExpanded = useCallback(
    (nextExpanded: boolean) => {
      setExpanded(nextExpanded);
      reportExpanded(nextExpanded);
    },
    [reportExpanded],
  );
  // Layout effect so the parent's wrapper resizes in the same paint as the
  // field appears or disappears.
  useLayoutEffect(() => {
    if (collapsible) reportExpanded(shouldExpand);
  }, [collapsible, reportExpanded, shouldExpand]);
  const collapseIfEmpty = useCallback(
    (currentValue = initialValue) => {
      if (collapsible && !currentValue) setSearchExpanded(false);
    },
    [collapsible, initialValue, setSearchExpanded],
  );

  if (collapsible && !shouldExpand) {
    return (
      <Button
        ariaLabel={label}
        ariaExpanded={false}
        variant={variants.secondary}
        size={sizes.compact}
        prefixIcon={<SearchIcon width="w-4" />}
        onClick={() => setSearchExpanded(true)}
        testId={`${id}-toggle`}
      />
    );
  }

  return (
    // The landmark is left unnamed on purpose: its only control already carries
    // `label`, and naming the region the same way makes "Search miners" resolve
    // to two elements for assistive tech and for getByLabelText alike.
    <div role="search" data-testid={`${id}-expanded`}>
      <Search
        id={id}
        label={label}
        variant="toolbar"
        initValue={initialValue}
        onChange={handleChange}
        onBlur={collapseIfEmpty}
        onClear={(previousValue) => {
          if (!previousValue) collapseIfEmpty("");
        }}
        showClearWhenEmpty={collapsible}
        shouldFocus={collapsible ? expanded : false}
        sanitize={sanitizeSearchQuery}
      />
    </div>
  );
};

export default ListSearchInput;
