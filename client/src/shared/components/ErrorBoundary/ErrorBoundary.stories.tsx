import * as React from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { action } from "storybook/actions";
import { DefaultErrorFallback } from "./DefaultErrorFallback";
import { ErrorBoundary } from "./ErrorBoundary";

const meta: Meta<typeof ErrorBoundary> = {
  title: "Shared/ErrorBoundary",
  component: ErrorBoundary,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof meta>;

// Component that throws an error for testing
const ThrowError = ({ shouldThrow = false }: { shouldThrow?: boolean }) => {
  if (shouldThrow) {
    throw new Error("This is a test error for the ErrorBoundary");
  }
  return <div>This component renders normally</div>;
};

// Custom fallback component
const CustomFallback = ({ error, onRetry }: { error?: Error; onRetry: () => void }) => (
  <div className="rounded-lg border border-red-300 bg-red-50 p-4">
    <h3 className="mb-2 font-semibold text-red-800">Custom Error Fallback</h3>
    <p className="mb-4 text-red-600">{error?.message || "An error occurred"}</p>
    <button onClick={onRetry} className="rounded bg-red-600 px-4 py-2 text-white hover:bg-red-700">
      Try Again
    </button>
  </div>
);

export const Default: Story = {
  args: {
    children: <ThrowError shouldThrow={true} />,
    onError: (error: Error, errorInfo: React.ErrorInfo) => {
      console.error("Error caught:", error);
      console.error("Error info:", errorInfo);
    },
  },
};

const DefaultFallbackWithStackTrace = () => (
  <DefaultErrorFallback
    showStackTrace={true}
    title="Something went wrong"
    description="An unexpected error occurred. Please try again."
    onRetry={action("onRetry")}
  />
);
export const WithoutStackTrace: Story = {
  args: {
    children: <ThrowError shouldThrow={true} />,
    fallback: DefaultFallbackWithStackTrace,
    onError: (error: Error, errorInfo: React.ErrorInfo) => {
      console.error("Error caught:", error);
      console.error("Error info:", errorInfo);
    },
  },
};

export const WithCustomFallback: Story = {
  args: {
    children: <ThrowError shouldThrow={true} />,
    fallback: CustomFallback,
    onError: (error: Error, errorInfo: React.ErrorInfo) => {
      console.error("Error caught:", error);
      console.error("Error info:", errorInfo);
    },
  },
};

export const WithErrorCallback: Story = {
  args: {
    children: <ThrowError shouldThrow={true} />,
    onError: (error: Error, errorInfo: React.ErrorInfo) => {
      console.error("Error caught:", error);
      console.error("Error info:", errorInfo);
    },
  },
};

export const NormalRendering: Story = {
  args: {
    children: <ThrowError shouldThrow={false} />,
    onError: undefined,
  },
};

export const WithResetKeys: Story = {
  args: {
    children: <ThrowError shouldThrow={true} />,
    resetKeys: [Date.now()], // This will reset the error boundary when the key changes
    onError: (error: Error, errorInfo: React.ErrorInfo) => {
      console.error("Error caught:", error);
      console.error("Error info:", errorInfo);
    },
  },
};
