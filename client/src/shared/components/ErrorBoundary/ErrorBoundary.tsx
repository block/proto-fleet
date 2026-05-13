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

// Build-scoped reload cap. Long-lived Fleet dashboards span deploys,
// so keying the counter by build commit means counts from prior
// builds don't drain the current build's recovery budget.
export const CHUNK_RELOAD_COUNTER_KEY = `proto-fleet:chunk-reload-count:${buildVersionInfo.commit}`;
export const CHUNK_RELOAD_MAX = 2;

// Orphan keys from earlier implementations (boolean flag, unscoped
// counter). Removed idempotently on every chunk-error path so a tab
// that survived past deploys doesn't keep stale entries.
const LEGACY_RELOAD_KEYS = ["proto-fleet:chunk-reload-attempted", "proto-fleet:chunk-reload-count"];

// ESM caches rejected dynamic imports — once a chunk URL 404s,
// React.lazy keeps returning the cached rejection. Detect the
// chunk-load error shapes (Vite, webpack, native ESM) so the reload
// path can pick up new hashes.
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
    // React's componentDidCatch type says Error, but the runtime passes
    // the raw thrown value — `throw null` / `throw "string"` reach here
    // unnormalized. getDerivedStateFromError normalizes for state;
    // mirror that here so downstream code (isChunkLoadError, onError)
    // can rely on .name / .message access.
    const normalized = error instanceof Error ? error : new Error(String(error));

    // Log the error to console in development
    if (import.meta.env.DEV) {
      console.error("ErrorBoundary caught an error:", normalized, errorInfo);
    }

    // Call the onError callback if provided
    this.props.onError?.(normalized, errorInfo);

    // Increment the counter and refresh until MAX, then the fallback
    // is sticky. resetError deliberately leaves the counter intact —
    // clearing it would let Retry bypass the cap when the CDN stays
    // broken.
    if (!isChunkLoadError(normalized) || typeof window === "undefined") return;

    // sessionStorage can throw in private-mode Safari or sandboxed
    // iframes. Without persistent state we can't cap reloads, and
    // reload-anyway is an infinite loop — wedged on the fallback is
    // the lesser evil.
    let count: number;
    try {
      for (const legacy of LEGACY_RELOAD_KEYS) {
        window.sessionStorage.removeItem(legacy);
      }
      const raw = window.sessionStorage.getItem(CHUNK_RELOAD_COUNTER_KEY);
      const parsed = parseInt(raw ?? "", 10);
      count = Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
      if (count >= CHUNK_RELOAD_MAX) return;
      window.sessionStorage.setItem(CHUNK_RELOAD_COUNTER_KEY, String(count + 1));
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
