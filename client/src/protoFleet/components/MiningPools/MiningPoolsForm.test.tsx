import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import MiningPoolsForm from ".";
import { OnboardingContext } from "@/protoFleet/features/onboarding/contexts/OnboardingContext";

const mocks = vi.hoisted(() => {
  return {
    pools: [] as { url: string; username: string }[],
  };
});

vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({
    pools: mocks.pools,
    createPool: vi.fn().mockResolvedValue({}),
    updatePool: vi.fn().mockResolvedValue({}),
    deletePool: vi.fn().mockResolvedValue({}),
  }),
}));

describe("MiningPoolsForm", () => {
  const buttonLabel = "Continue";

  test("renders default and backup pool rows", () => {
    const { getByText, getAllByText } = render(
      <OnboardingContext.Provider value={{ status: null, refetch: vi.fn() }}>
        <MiningPoolsForm buttonLabel="Save" onSaveDone={vi.fn()} />,
      </OnboardingContext.Provider>,
    );

    expect(getByText("Default pool")).toBeInTheDocument();
    expect(getByText("Backup pool #1")).toBeInTheDocument();
    expect(getByText("Backup pool #2")).toBeInTheDocument();
    expect(getAllByText("Not configured")).toHaveLength(3);
  });

  test("displays warning when default pool is invalid", () => {
    const { getByTestId, getByRole } = render(
      <OnboardingContext.Provider value={{ status: null, refetch: vi.fn() }}>
        <MiningPoolsForm buttonLabel={buttonLabel} onSaveDone={vi.fn()} />
      </OnboardingContext.Provider>,
    );

    const saveButton = getByRole("button", { name: buttonLabel });
    fireEvent.click(saveButton);
    expect(getByTestId("warn-default-pool-callout")).toBeInTheDocument();
  });

  test("disables save button while loading", async () => {
    // ensure we have a valid default pool
    mocks.pools = [{ url: "https://example.com", username: "user" }];

    const { getByRole } = render(
      <OnboardingContext.Provider value={{ status: null, refetch: vi.fn() }}>
        <MiningPoolsForm buttonLabel={buttonLabel} onSaveDone={vi.fn()} />,
      </OnboardingContext.Provider>,
    );

    const saveButton = getByRole("button", { name: buttonLabel });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(saveButton).toBeDisabled();
    });
  });

  test("calls onSaveDone after successful save", async () => {
    const mockOnSaveRequested = vi.fn();
    const mockOnSaveDone = vi.fn();

    const { getByRole } = render(
      <OnboardingContext.Provider value={{ status: null, refetch: vi.fn() }}>
        <MiningPoolsForm
          buttonLabel={buttonLabel}
          onSaveRequested={mockOnSaveRequested}
          onSaveDone={mockOnSaveDone}
        />
      </OnboardingContext.Provider>,
    );

    const saveButton = getByRole("button", { name: buttonLabel });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(mockOnSaveRequested).toHaveBeenCalled();
      expect(mockOnSaveDone).toHaveBeenCalled();
    });
  });
});
