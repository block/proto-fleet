import * as React from "react";
import { DefaultErrorFallback } from "./DefaultErrorFallback";

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

// sessionStorage flag so a stale-deploy reload cannot loop if the new
// chunks have not yet propagated to the CDN edge.
const CHUNK_RELOAD_SESSION_KEY = "proto-fleet:chunk-reload-attempted";

// React.lazy throws via the ESM module loader, which caches rejected
// dynamic imports — once a chunk URL 404s, every subsequent import() for
// the same specifier returns the cached rejection. Detect those error
// shapes (Vite native ESM, webpack-style, dynamic-module fetch failures)
// and reload once per session to pick up the new chunk hashes.
const isChunkLoadError = (error: Error): boolean => {
  if (error.name === "ChunkLoadError") return true;
  const message = error.message || "";
  return (
    /Loading (CSS )?chunk \d+ failed/i.test(message) ||
    /Failed to fetch dynamically imported module/i.test(message) ||
    /error loading dynamically imported module/i.test(message)
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

    // Chunk-load failure recovery: reload once to refresh the module
    // registry. Any subsequent chunk failure in the same session falls
    // through to the normal fallback so the user can recover manually.
    if (isChunkLoadError(error) && typeof window !== "undefined") {
      try {
        if (window.sessionStorage.getItem(CHUNK_RELOAD_SESSION_KEY)) return;
        window.sessionStorage.setItem(CHUNK_RELOAD_SESSION_KEY, "1");
      } catch {
        // sessionStorage can throw in private-mode Safari or sandboxed
        // iframes; fall through to the normal fallback in that case.
        return;
      }
      window.location.reload();
    }
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps, prevState: ErrorBoundaryState): void {
    // Reset error state when resetKeys change
    if (prevState.hasError && prevProps.resetKeys !== this.props.resetKeys) {
      this.setState({ hasError: false, error: undefined });
    }
  }

  resetError = (): void => {
    // Clear the chunk-reload guard so a fresh user-initiated retry after
    // an auto-reload attempt can trigger another reload if needed.
    if (typeof window !== "undefined") {
      try {
        window.sessionStorage.removeItem(CHUNK_RELOAD_SESSION_KEY);
      } catch {
        // ignored — same private-mode edge case as the reload path
      }
    }
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
