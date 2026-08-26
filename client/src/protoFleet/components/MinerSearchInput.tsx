import { useCallback, useEffect, useRef } from "react";

import Search from "@/shared/components/Search";

const SEARCH_DEBOUNCE_MS = 250;

// Leading whitespace never narrows a search, so it is dropped at the input,
// where `sanitize` applies it to the displayed text and the emitted value
// together. Trailing whitespace has to survive: stripping it would erase the
// space the moment it is typed and make multi-word queries impossible.
const trimLeadingWhitespace = (value: string) => value.trimStart();

interface MinerSearchInputProps {
  initialValue?: string;
  onQueryChange: (query: string) => void;
  compact?: boolean;
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
  compact = true,
  id = "miner-search",
}: MinerSearchInputProps) => {
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // The pending timer must survive a new `onQueryChange` identity (the fleet
  // table rebuilds it on every navigation), so the callback is read from a ref
  // at fire time instead of being captured per keystroke.
  const onQueryChangeRef = useRef(onQueryChange);
  onQueryChangeRef.current = onQueryChange;

  const handleChange = useCallback((value: string) => {
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    timeoutRef.current = setTimeout(() => {
      onQueryChangeRef.current(value);
    }, SEARCH_DEBOUNCE_MS);
  }, []);

  // Unmount-only: keying this on `onQueryChange` would cancel a pending search
  // whenever an unrelated navigation changed the callback's identity.
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
      compact={compact}
      initValue={initialValue}
      onChange={handleChange}
      sanitize={trimLeadingWhitespace}
    />
  );
};

export default MinerSearchInput;
