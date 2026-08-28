import { useCallback, useEffect, useRef } from "react";

import Search from "@/shared/components/Search";

const SEARCH_DEBOUNCE_MS = 250;

const MAX_SEARCH_QUERY_CODE_POINTS = 255;

// Leading whitespace never narrows a search, so it is dropped at the input,
// where `sanitize` applies it to the displayed text and the emitted value
// together. Trailing whitespace has to survive: stripping it would erase the
// space the moment it is typed and make multi-word queries impossible. Cap by
// Unicode code point rather than UTF-16 code unit so this matches the proto
// validator and the server's utf8.RuneCountInString check.
const sanitizeSearchQuery = (value: string) =>
  Array.from(value.trimStart()).slice(0, MAX_SEARCH_QUERY_CODE_POINTS).join("");

interface MinerSearchInputProps {
  initialValue?: string;
  onQueryChange: (query: string) => void;
  /** Fired synchronously on every keystroke, ahead of the debounce.
   *
   * Consumers that treat "a search is active" as a safety condition have to use
   * this rather than `onQueryChange`: for the debounce interval the applied
   * filter still reads as empty, so a selection gated on the applied filter
   * stays armed while the field already shows a query. */
  onQueryInput?: (query: string) => void;
  id?: string;
}

/** Search control for miner lists. The visible input updates immediately while
 * requests are debounced so typing does not issue one RPC per keystroke.
 *
 * The query is emitted exactly as typed. Callers persist it to the URL and feed
 * it back as `initialValue`, and the input re-seeds itself from that prop, so
 * emitting a normalized form would overwrite the text mid-entry — trimming here
 * ate the space in "rack 7" whenever the debounce fired between the two words.
 * Trimming belongs at the consumers, which already do it: the server trims
 * search_query, and the miner list trims when reading the URL param. */
const MinerSearchInput = ({
  initialValue = "",
  onQueryChange,
  onQueryInput,
  id = "miner-search",
}: MinerSearchInputProps) => {
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

  return (
    <Search
      id={id}
      label="Search miners"
      variant="toolbar"
      initValue={initialValue}
      onChange={handleChange}
      sanitize={sanitizeSearchQuery}
    />
  );
};

export default MinerSearchInput;
