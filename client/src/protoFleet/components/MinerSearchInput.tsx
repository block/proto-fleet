import { useCallback, useEffect, useRef } from "react";

import Search from "@/shared/components/Search";

const SEARCH_DEBOUNCE_MS = 250;

interface MinerSearchInputProps {
  initialValue?: string;
  onQueryChange: (query: string) => void;
  compact?: boolean;
  id?: string;
}

/** Search control for miner lists. The visible input updates immediately while
 * requests are debounced so typing does not issue one RPC per keystroke. */
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
      onQueryChangeRef.current(value.trim());
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

  return <Search id={id} label="Search miners" compact={compact} initValue={initialValue} onChange={handleChange} />;
};

export default MinerSearchInput;
