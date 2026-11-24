import { useState } from "react";

/**
 * Remembers the HA controllers a source has reported, so the picker stays
 * available when the selected controller can't be read.
 */
export function useKnownControllers(reported: string[] | undefined) {
  const [known, setKnown] = useState<string[]>([]);
  if (reported?.length && reported.join("\n") !== known.join("\n")) setKnown(reported);
  return known;
}
