import { type LoaderFunction, redirect } from "react-router-dom";

// Preserves `search + hash` so deep-links carrying filter state survive the
// redirect.
const buildRedirect = (target: string): LoaderFunction => {
  return ({ request }) => {
    const url = new URL(request.url);
    return redirect(`${target}${url.search}${url.hash}`);
  };
};

export const minersRedirectLoader = buildRedirect("/fleet/miners");
export const racksRedirectLoader = buildRedirect("/fleet/racks");
export const sitesRedirectLoader = buildRedirect("/fleet/sites");
