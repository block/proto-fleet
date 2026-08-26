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

  const handleChange = useCallback(
    (value: string) => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
      timeoutRef.current = setTimeout(() => {
        onQueryChange(value.trim());
      }, SEARCH_DEBOUNCE_MS);
    },
    [onQueryChange],
  );

  useEffect(
    () => () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
    },
    [onQueryChange],
  );

  return <Search id={id} label="Search miners" compact={compact} initValue={initialValue} onChange={handleChange} />;
};

export default MinerSearchInput;
