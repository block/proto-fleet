// Schedule lazy-route imports at idle so chunks warm before the user
// navigates. React.lazy reuses the in-flight or resolved promise, so a
// prefetched route renders without a Suspense fallback; bundler dedup
// makes overlapping calls free. Returns a CancelPrefetch — React
// consumers should `return prefetchRoutes(...)` from useEffect to
// cancel the pending idle callback on unmount.

export type RouteImporter = () => Promise<unknown>;

type CancelPrefetch = () => void;

const NOOP_CANCEL: CancelPrefetch = () => undefined;

const schedule = (cb: () => void): CancelPrefetch => {
  if (typeof window === "undefined") return NOOP_CANCEL;
  if (typeof window.requestIdleCallback === "function") {
    const handle = window.requestIdleCallback(cb, { timeout: 2000 });
    return () => window.cancelIdleCallback(handle);
  }
  // Safari < 16.4 fallback. 500ms balances landing past first paint
  // (iOS < 16.4 phones can paint at 400-800ms under load) against
  // keeping the warming win.
  const handle = setTimeout(cb, 500);
  return () => clearTimeout(handle);
};

export const prefetchRoutes = (importers: readonly RouteImporter[]): CancelPrefetch => {
  return schedule(() => {
    for (const importer of importers) {
      // Log rejections so a stale-deploy 404 wave is visible in ops,
      // not silent. ESM caches the rejected promise, so recovery
      // happens in ErrorBoundary's chunk-failure reload path — not
      // via the import itself.
      importer().catch((err) => {
        console.error("[prefetchRoutes] chunk prefetch failed:", err);
      });
    }
  });
};
