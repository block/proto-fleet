import { type LoaderFunction, redirect } from "react-router-dom";

// Permanent redirects from the legacy /miners and /racks routes to their
// Fleet-tab homes. Preserving `search + hash` is the contract — dashboard
// issue cards, rack-overview "view miners" links, and other deep-link
// entry points pass filter state through the query string. A loader that
// returned `redirect("/fleet/miners")` would silently drop that state.
const buildRedirect = (target: string): LoaderFunction => {
  return ({ request }) => {
    const url = new URL(request.url);
    return redirect(`${target}${url.search}${url.hash}`);
  };
};

export const minersRedirectLoader = buildRedirect("/fleet/miners");
export const racksRedirectLoader = buildRedirect("/fleet/racks");
