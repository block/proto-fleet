import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ErrorBoundary } from "./ErrorBoundary";

const CHUNK_RELOAD_SESSION_KEY = "proto-fleet:chunk-reload-attempted";

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
      window.sessionStorage.removeItem(CHUNK_RELOAD_SESSION_KEY);
      reloadSpy = vi.fn();
      Object.defineProperty(window, "location", {
        configurable: true,
        value: { ...window.location, reload: reloadSpy },
      });
    });

    afterEach(() => {
      window.sessionStorage.removeItem(CHUNK_RELOAD_SESSION_KEY);
    });

    it("reloads the page when a ChunkLoadError is caught", () => {
      render(
        <ErrorBoundary>
          <ThrowChunkError message="Loading chunk 42 failed." />
        </ErrorBoundary>,
      );

      expect(reloadSpy).toHaveBeenCalledTimes(1);
      expect(window.sessionStorage.getItem(CHUNK_RELOAD_SESSION_KEY)).toBe("1");
    });

    it("reloads on a Vite 'Failed to fetch dynamically imported module' error", () => {
      render(
        <ErrorBoundary>
          <ThrowDynamicImportError message="Failed to fetch dynamically imported module: /assets/Foo-abc123.js" />
        </ErrorBoundary>,
      );

      expect(reloadSpy).toHaveBeenCalledTimes(1);
    });

    it("does not reload a second time within the same session", () => {
      window.sessionStorage.setItem(CHUNK_RELOAD_SESSION_KEY, "1");

      render(
        <ErrorBoundary>
          <ThrowChunkError message="Loading chunk 42 failed." />
        </ErrorBoundary>,
      );

      expect(reloadSpy).not.toHaveBeenCalled();
      expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    });

    it("clears the chunk-reload guard when the user retries via the fallback", () => {
      window.sessionStorage.setItem(CHUNK_RELOAD_SESSION_KEY, "1");

      // Use a non-chunk error to isolate the resetError path: the boundary
      // shows its fallback without triggering the reload guard, so the
      // sessionStorage clear from a Retry click is directly observable.
      render(
        <ErrorBoundary>
          <ThrowError shouldThrow={true} />
        </ErrorBoundary>,
      );

      fireEvent.click(screen.getByText("Retry"));

      expect(window.sessionStorage.getItem(CHUNK_RELOAD_SESSION_KEY)).toBeNull();
    });

    it("does not reload for unrelated errors", () => {
      render(
        <ErrorBoundary>
          <ThrowError shouldThrow={true} />
        </ErrorBoundary>,
      );

      expect(reloadSpy).not.toHaveBeenCalled();
      expect(window.sessionStorage.getItem(CHUNK_RELOAD_SESSION_KEY)).toBeNull();
    });
  });
});
