import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CHUNK_RELOAD_COUNTER_KEY, CHUNK_RELOAD_MAX, ErrorBoundary } from "./ErrorBoundary";

// Component that throws an error for testing
const ThrowError = ({ shouldThrow = false }: { shouldThrow?: boolean }) => {
  if (shouldThrow) {
    throw new Error("Test error message");
  }
  return <div>Normal content</div>;
};

const ThrowChunkError = ({ message }: { message: string }) => {
  const err = new Error(message);
  err.name = "ChunkLoadError";
  throw err;
};

const ThrowDynamicImportError = ({ message }: { message: string }) => {
  throw new Error(message);
};

// Custom fallback component for testing
const CustomFallback = ({ error, onRetry }: { error?: Error; onRetry: () => void }) => (
  <div>
    <h2>Custom Error: {error?.message}</h2>
    <button onClick={onRetry}>Reset</button>
  </div>
);

describe("ErrorBoundary", () => {
  beforeEach(() => {
    // Suppress console.error for expected errors in tests
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders children normally when no error occurs", () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={false} />
      </ErrorBoundary>,
    );

    expect(screen.getByText("Normal content")).toBeInTheDocument();
  });

  it("renders default fallback when error occurs", () => {
    render(
      <ErrorBoundary>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>,
    );

    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    expect(screen.getByText("Test error message")).toBeInTheDocument();
    expect(screen.getByText("Retry")).toBeInTheDocument();
  });

  it("renders custom fallback when provided", () => {
    render(
      <ErrorBoundary fallback={CustomFallback}>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>,
    );

    expect(screen.getByText("Custom Error: Test error message")).toBeInTheDocument();
    expect(screen.getByText("Reset")).toBeInTheDocument();
  });

  it("calls onError callback when error occurs", () => {
    const onError = vi.fn();

    render(
      <ErrorBoundary onError={onError}>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>,
    );

    expect(onError).toHaveBeenCalledWith(
      expect.any(Error),
      expect.objectContaining({
        componentStack: expect.any(String),
      }),
    );
  });

  it("resets error state when resetKeys change", () => {
    const { rerender } = render(
      <ErrorBoundary resetKeys={[1]}>
        <ThrowError shouldThrow={true} />
      </ErrorBoundary>,
    );

    // Error should be displayed
    expect(screen.getByText("Something went wrong")).toBeInTheDocument();

    // Change resetKeys
    rerender(
      <ErrorBoundary resetKeys={[2]}>
        <ThrowError shouldThrow={false} />
      </ErrorBoundary>,
    );

    // Should show normal content again
    expect(screen.getByText("Normal content")).toBeInTheDocument();
  });

  it("handles non-Error objects gracefully", () => {
    const NonErrorThrower = () => {
      throw "String error";
    };

    render(
      <ErrorBoundary>
        <NonErrorThrower />
      </ErrorBoundary>,
    );

    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    expect(screen.getByText("String error")).toBeInTheDocument();
  });

  describe("chunk-load failure recovery", () => {
    let reloadSpy: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      window.sessionStorage.removeItem(CHUNK_RELOAD_COUNTER_KEY);
      reloadSpy = vi.fn();
      Object.defineProperty(window, "location", {
        configurable: true,
        value: { ...window.location, reload: reloadSpy },
      });
    });

    afterEach(() => {
      window.sessionStorage.removeItem(CHUNK_RELOAD_COUNTER_KEY);
    });

    it("reloads the page when a ChunkLoadError is caught", () => {
      render(
        <ErrorBoundary>
          <ThrowChunkError message="Loading chunk 42 failed." />
        </ErrorBoundary>,
      );

      expect(reloadSpy).toHaveBeenCalledTimes(1);
      expect(window.sessionStorage.getItem(CHUNK_RELOAD_COUNTER_KEY)).toBe("1");
    });

    it("reloads on a webpack-style 'Loading chunk N failed' error with no err.name override", () => {
      render(
        <ErrorBoundary>
          <ThrowDynamicImportError message="Loading chunk 5 failed." />
        </ErrorBoundary>,
      );

      expect(reloadSpy).toHaveBeenCalledTimes(1);
    });

    it("reloads on a webpack-style 'Loading CSS chunk N failed' error", () => {
      render(
        <ErrorBoundary>
          <ThrowDynamicImportError message="Loading CSS chunk 3 failed." />
        </ErrorBoundary>,
      );

      expect(reloadSpy).toHaveBeenCalledTimes(1);
    });

    it("reloads on a Vite 'Failed to fetch dynamically imported module' error", () => {
      render(
        <ErrorBoundary>
          <ThrowDynamicImportError message="Failed to fetch dynamically imported module: /assets/Foo-abc123.js" />
        </ErrorBoundary>,
      );

      expect(reloadSpy).toHaveBeenCalledTimes(1);
    });

    it("reloads on an 'error loading dynamically imported module' error", () => {
      render(
        <ErrorBoundary>
          <ThrowDynamicImportError message="error loading dynamically imported module: /assets/Bar-def456.js" />
        </ErrorBoundary>,
      );

      expect(reloadSpy).toHaveBeenCalledTimes(1);
    });

    it("stops reloading once the per-session counter reaches CHUNK_RELOAD_MAX", () => {
      window.sessionStorage.setItem(CHUNK_RELOAD_COUNTER_KEY, String(CHUNK_RELOAD_MAX));

      render(
        <ErrorBoundary>
          <ThrowChunkError message="Loading chunk 42 failed." />
        </ErrorBoundary>,
      );

      expect(reloadSpy).not.toHaveBeenCalled();
      expect(screen.getByText("Something went wrong")).toBeInTheDocument();
      expect(window.sessionStorage.getItem(CHUNK_RELOAD_COUNTER_KEY)).toBe(String(CHUNK_RELOAD_MAX));
    });

    it("Retry click does not reset the chunk-reload counter (bounded-loop guarantee)", () => {
      window.sessionStorage.setItem(CHUNK_RELOAD_COUNTER_KEY, String(CHUNK_RELOAD_MAX));

      // Non-chunk error so the fallback renders without re-triggering the
      // reload path; the assertion is purely about resetError's contract.
      render(
        <ErrorBoundary>
          <ThrowError shouldThrow={true} />
        </ErrorBoundary>,
      );

      fireEvent.click(screen.getByText("Retry"));

      expect(window.sessionStorage.getItem(CHUNK_RELOAD_COUNTER_KEY)).toBe(String(CHUNK_RELOAD_MAX));
    });

    it("Retry on a still-failing chunk does not exceed the reload cap", () => {
      // Simulate state right at the cap: an auto-reload already happened
      // and the CDN is still broken. User clicks Retry; the child throws
      // the cached rejection again. Counter must hold the line.
      window.sessionStorage.setItem(CHUNK_RELOAD_COUNTER_KEY, String(CHUNK_RELOAD_MAX));

      render(
        <ErrorBoundary>
          <ThrowChunkError message="Loading chunk 42 failed." />
        </ErrorBoundary>,
      );

      // First render: fallback (counter at MAX, no reload).
      expect(reloadSpy).not.toHaveBeenCalled();

      fireEvent.click(screen.getByText("Retry"));

      // After Retry the child re-throws but the counter is still at MAX,
      // so no additional reload fires.
      expect(reloadSpy).not.toHaveBeenCalled();
      expect(window.sessionStorage.getItem(CHUNK_RELOAD_COUNTER_KEY)).toBe(String(CHUNK_RELOAD_MAX));
    });

    it("does not reload for unrelated errors", () => {
      render(
        <ErrorBoundary>
          <ThrowError shouldThrow={true} />
        </ErrorBoundary>,
      );

      expect(reloadSpy).not.toHaveBeenCalled();
      expect(window.sessionStorage.getItem(CHUNK_RELOAD_COUNTER_KEY)).toBeNull();
    });
  });
});
