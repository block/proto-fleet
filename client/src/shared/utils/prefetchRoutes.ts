// Schedule lazy-route imports at browser idle time so each route's chunk is
// warmed in the HTTP cache before the user navigates to it. React.lazy reuses
// the in-flight or resolved promise, so a navigation that hits a prefetched
// route resolves without a Suspense fallback. The bundler dedupes calls to
// the same import factory, so calling prefetchRoutes() more than once with
// overlapping importers is safe and effectively free.

type RouteImporter = () => Promise<unknown>;

const schedule = (cb: () => void): void => {
  if (typeof window === "undefined") return;
  if (typeof window.requestIdleCallback === "function") {
    window.requestIdleCallback(cb, { timeout: 2000 });
  } else {
    // Safari < 16.4 path: defer past first paint without monopolising the
    // main thread.
    setTimeout(cb, 200);
  }
};

export const prefetchRoutes = (importers: RouteImporter[]): void => {
  schedule(() => {
    for (const importer of importers) {
      // Swallow errors: a prefetch failure shouldn't become an unhandled
      // rejection. The real navigation re-invokes the import and surfaces
      // any error through the Suspense/ErrorBoundary path.
      importer().catch(() => undefined);
    }
  });
};
