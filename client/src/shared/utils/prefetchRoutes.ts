// Schedule lazy-route imports at browser idle time so each route's chunk is
// warmed in the HTTP cache before the user navigates to it. React.lazy reuses
// the in-flight or resolved promise, so a navigation that hits a prefetched
// route resolves without a Suspense fallback. The bundler dedupes calls to
// the same import factory, so calling prefetchRoutes() more than once with
// overlapping importers is safe and effectively free.

type RouteImporter = () => Promise<unknown>;

type CancelPrefetch = () => void;

const NOOP_CANCEL: CancelPrefetch = () => undefined;

const schedule = (cb: () => void): CancelPrefetch => {
  if (typeof window === "undefined") return NOOP_CANCEL;
  if (typeof window.requestIdleCallback === "function") {
    const handle = window.requestIdleCallback(cb, { timeout: 2000 });
    return () => window.cancelIdleCallback(handle);
  }
  // Safari < 16.4 path. 500ms is a compromise: long enough to land past first
  // paint on the older devices that hit this fallback (iPhones stuck on iOS
  // < 16.4 can paint at 400-800ms under load), short enough that the warming
  // win still pays off.
  const handle = setTimeout(cb, 500);
  return () => clearTimeout(handle);
};

export const prefetchRoutes = (importers: RouteImporter[]): CancelPrefetch => {
  return schedule(() => {
    for (const importer of importers) {
      // Log rejections so a deploy that invalidates chunk hashes (and 404s
      // every prefetch on every open tab) is visible in ops, not silent. The
      // real navigation re-invokes the import and surfaces any error through
      // the Suspense/ErrorBoundary path; this log is observability-only.
      importer().catch((err) => {
        console.error("[prefetchRoutes] chunk prefetch failed:", err);
      });
    }
  });
};
