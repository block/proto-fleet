import { useContext } from "react";
import { UNSAFE_DataRouterContext, UNSAFE_DataRouterStateContext } from "react-router";

export type PageBackground = "surface-5" | "surface-base";

const bgClassMap: Record<PageBackground, string> = {
  "surface-5": "bg-surface-5 dark:bg-surface-base",
  "surface-base": "bg-surface-base",
};

export const usePageBackground = () => {
  // Read the data router state directly via context instead of useMatches(),
  // so we can safely fall back when rendered under a plain <MemoryRouter> (tests/storybook).
  const dataRouterContext = useContext(UNSAFE_DataRouterContext);
  const state = useContext(UNSAFE_DataRouterStateContext);

  let bg: PageBackground = "surface-base";
  if (dataRouterContext && state) {
    const matches = state.matches;
    bg = (matches[matches.length - 1]?.route?.handle as { bg?: PageBackground } | undefined)?.bg ?? "surface-base";
  }

  return { bg, bgClass: bgClassMap[bg] };
};
