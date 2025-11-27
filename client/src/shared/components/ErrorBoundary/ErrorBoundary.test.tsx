import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ErrorBoundary } from "./ErrorBoundary";

// Component that throws an error for testing
const ThrowError = ({ shouldThrow = false }: { shouldThrow?: boolean }) => {
  if (shouldThrow) {
    throw new Error("Test error message");
  }
  return <div>Normal content</div>;
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
});
