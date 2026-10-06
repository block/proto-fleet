import { type LoaderFunction, redirect } from "react-router-dom";

import { scopedPath } from "@/protoFleet/routing/siteScope";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const SITE_FILTER_RE = /^\d+$/;

// Preserves `search + hash` so deep-links carrying filter state survive the
// redirect.
const buildRedirect = (target: string): LoaderFunction => {
  return ({ request }) => {
    const url = new URL(request.url);
    const hasExplicitSiteFilter = url.searchParams
      .getAll("site")
      .flatMap((value) => value.split(","))
      .some((value) => SITE_FILTER_RE.test(value.trim()));
    const activeSite = hasExplicitSiteFilter ? DEFAULT_ACTIVE_SITE : useFleetStore.getState().ui.activeSite;
    const scopedTarget = scopedPath(target, activeSite);
    return redirect(`${scopedTarget}${url.search}${url.hash}`);
  };
};

export const minersRedirectLoader = buildRedirect("/fleet/miners");
export const racksRedirectLoader = buildRedirect("/fleet/racks");
export const sitesRedirectLoader = buildRedirect("/fleet/sites");
