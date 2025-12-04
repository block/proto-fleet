import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeactivateUserDialog from "./DeactivateUserDialog";

const mockOnConfirm = vi.fn();
const mockOnDismiss = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("DeactivateUserDialog", () => {
  it("renders with username", () => {
    const { getByText } = render(
      <DeactivateUserDialog
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText("Deactivate member?")).toBeInTheDocument();
    expect(getByText(/john_doe/)).toBeInTheDocument();
    expect(getByText("Cancel")).toBeInTheDocument();
    expect(getByText("Confirm deactivation")).toBeInTheDocument();
  });

  it("calls onDismiss when Cancel is clicked", () => {
    const { getByText } = render(
      <DeactivateUserDialog
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    fireEvent.click(getByText("Cancel"));
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("calls onConfirm when Confirm deactivation is clicked", () => {
    const { getByText } = render(
      <DeactivateUserDialog
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    fireEvent.click(getByText("Confirm deactivation"));
    expect(mockOnConfirm).toHaveBeenCalled();
  });

  it("disables confirm button when isSubmitting is true", () => {
    const { getByText } = render(
      <DeactivateUserDialog
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={true}
      />,
    );

    const confirmButton = getByText("Confirm deactivation").closest("button");
    expect(confirmButton).toBeDisabled();
  });

  it("displays warning message with user details", () => {
    const { getByText } = render(
      <DeactivateUserDialog
        username="jane_smith"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText(/They will be hidden and removed from your account/)).toBeInTheDocument();
    expect(getByText(/This action cannot be undone/)).toBeInTheDocument();
  });

  it("handles long usernames", () => {
    const longUsername = "user_with_a_very_long_username_for_testing";
    const { getByText } = render(
      <DeactivateUserDialog
        username={longUsername}
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText(new RegExp(longUsername))).toBeInTheDocument();
  });
});
