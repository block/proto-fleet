import * as React from "react";
import { DefaultErrorFallback } from "./DefaultErrorFallback";
import { buildVersionInfo } from "@/shared/utils/version";

export interface ErrorBoundaryProps {
  children: React.ReactNode;
  fallback?: React.ComponentType<{ error?: Error; onRetry: () => void }>;
  onError?: (error: Error, errorInfo: React.ErrorInfo) => void;
  resetKeys?: unknown[];
}

export interface ErrorBoundaryState {
  hasError: boolean;
  error?: Error;
}

// Build-scoped reload guard: cap chunk-load-driven reloads at MAX per
// session per build. Including the build commit in the key prevents
// counts from previous builds from draining the recovery budget for the
// currently-loaded build — long-lived Fleet dashboards can span multiple
// deploys, and without scoping the counter accumulates across them and
// silently stops auto-recovering after CHUNK_RELOAD_MAX total reloads.
export const CHUNK_RELOAD_COUNTER_KEY = `proto-fleet:chunk-reload-count:${buildVersionInfo.commit}`;
export const CHUNK_RELOAD_MAX = 2;

// Orphan keys from earlier implementations (boolean flag, unscoped
// counter). Cleared on the first chunk-error path so DevTools storage
// does not accumulate stale entries across deploys.
const LEGACY_RELOAD_KEYS = ["proto-fleet:chunk-reload-attempted", "proto-fleet:chunk-reload-count"];

// React.lazy throws via the ESM module loader, which caches rejected
// dynamic imports — once a chunk URL 404s, every subsequent import() for
// the same specifier returns the cached rejection. Detect those error
// shapes (Vite native ESM, webpack-style, dynamic-module fetch failures)
// and reload to pick up the new chunk hashes.
const isChunkLoadError = (error: Error): boolean => {
  if (error.name === "ChunkLoadError") return true;
  const message = error.message || "";
  return (
    /Loading (CSS )?chunk \d+ failed/i.test(message) ||
    /Failed to fetch dynamically imported module/i.test(message) ||
    /error loading dynamically imported module/i.test(message) ||
    /Importing a module script failed/i.test(message) ||
    /Unable to preload CSS/i.test(message)
  );
};

export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(error: unknown): ErrorBoundaryState {
    // Update state so the next render will show the fallback UI
    return {
      hasError: true,
      error: error instanceof Error ? error : new Error(String(error)),
    };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo): void {
    // Log the error to console in development
    if (import.meta.env.DEV) {
      console.error("ErrorBoundary caught an error:", error, errorInfo);
    }

    // Call the onError callback if provided
    this.props.onError?.(error, errorInfo);

    // Chunk-load failure recovery. Increment the per-session reload
    // counter and refresh until MAX is reached; after that the fallback
    // is sticky and the user can manually F5 / close the tab. The
    // counter is deliberately not cleared by `resetError` — letting the
    // user re-trigger the reload via Retry would defeat the cap when the
    // CDN stays broken across the auto-reload.
    if (!isChunkLoadError(error) || typeof window === "undefined") return;

    // sessionStorage can throw in private-mode Safari or sandboxed
    // iframes. Without persistent state we cannot cap the reload count,
    // and reloading-anyway turns every chunk error into an infinite
    // refresh loop in exactly those contexts — leaving the user wedged
    // on the fallback is the lesser evil.
    let count: number;
    try {
      const raw = window.sessionStorage.getItem(CHUNK_RELOAD_COUNTER_KEY);
      const parsed = parseInt(raw ?? "", 10);
      count = Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
      if (count >= CHUNK_RELOAD_MAX) return;
      window.sessionStorage.setItem(CHUNK_RELOAD_COUNTER_KEY, String(count + 1));
      for (const legacy of LEGACY_RELOAD_KEYS) {
        window.sessionStorage.removeItem(legacy);
      }
    } catch {
      return;
    }
    window.location.reload();
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps, prevState: ErrorBoundaryState): void {
    // Reset error state when resetKeys change
    if (prevState.hasError && prevProps.resetKeys !== this.props.resetKeys) {
      this.setState({ hasError: false, error: undefined });
    }
  }

  resetError = (): void => {
    this.setState({ hasError: false, error: undefined });
  };

  render(): React.ReactNode {
    if (this.state.hasError) {
      // Use custom fallback if provided
      if (this.props.fallback) {
        const FallbackComponent = this.props.fallback;
        return <FallbackComponent error={this.state.error} onRetry={this.resetError} />;
      }

      // Default fallback using DefaultErrorFallback
      return (
        <DefaultErrorFallback
          title="Something went wrong"
          description={"An unexpected error occurred. Please try again."}
          error={this.state.error}
          onRetry={this.resetError}
        />
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
