import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import MiningPoolsForm from ".";

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

vi.mock("@/protoFleet/api/useOnboardedStatus", () => ({
  useOnboardedStatus: () => ({
    poolConfigured: false,
    devicePaired: false,
    statusLoaded: true,
    refetch: vi.fn(),
  }),
}));

describe("MiningPoolsForm", () => {
  const buttonLabel = "Continue";

  test("renders default and backup pool rows", () => {
    const { getByText, getAllByText } = render(<MiningPoolsForm buttonLabel="Save" onSaveDone={vi.fn()} />);

    expect(getByText("Default pool")).toBeInTheDocument();
    expect(getByText("Backup pool #1")).toBeInTheDocument();
    expect(getByText("Backup pool #2")).toBeInTheDocument();
    expect(getAllByText("Not configured")).toHaveLength(3);
  });

  test("displays warning when default pool is invalid", () => {
    const { getByTestId, getByRole } = render(<MiningPoolsForm buttonLabel={buttonLabel} onSaveDone={vi.fn()} />);

    const saveButton = getByRole("button", { name: buttonLabel });
    fireEvent.click(saveButton);
    expect(getByTestId("warn-default-pool-callout")).toBeInTheDocument();
  });

  test("disables save button while loading", async () => {
    // ensure we have a valid default pool
    mocks.pools = [{ url: "https://example.com", username: "user" }];

    const { getByRole } = render(<MiningPoolsForm buttonLabel={buttonLabel} onSaveDone={vi.fn()} />);

    await waitFor(() => {
      // When pools are initialized, at least one "Not configured" should change
      const notConfiguredCount = document.body.textContent?.match(/Not configured/g)?.length || 0;
      expect(notConfiguredCount).toBeLessThan(3);
    });

    const saveButton = getByRole("button", { name: buttonLabel });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(saveButton).toBeDisabled();
    });
  });

  test("calls onSaveDone after successful save", async () => {
    const mockOnSaveRequested = vi.fn();
    const mockOnSaveDone = vi.fn();

    // Ensure we have a valid default pool
    mocks.pools = [{ url: "https://example.com", username: "user" }];

    const { getByRole } = render(
      <MiningPoolsForm buttonLabel={buttonLabel} onSaveRequested={mockOnSaveRequested} onSaveDone={mockOnSaveDone} />,
    );

    await waitFor(() => {
      const notConfiguredCount = document.body.textContent?.match(/Not configured/g)?.length || 0;
      expect(notConfiguredCount).toBeLessThan(3);
    });

    const saveButton = getByRole("button", { name: buttonLabel });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(mockOnSaveRequested).toHaveBeenCalled();
      expect(mockOnSaveDone).toHaveBeenCalled();
    });
  });
});
