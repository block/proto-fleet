import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeactivateUserModal from "./DeactivateUserModal";

const mockOnConfirm = vi.fn();
const mockOnDismiss = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("DeactivateUserModal", () => {
  it("renders with username", () => {
    const { getByText } = render(
      <DeactivateUserModal
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
      <DeactivateUserModal
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
      <DeactivateUserModal
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    fireEvent.click(getByText("Confirm deactivation"));
    expect(mockOnConfirm).toHaveBeenCalled();
  });

  it("shows loading state when isSubmitting is true", () => {
    const { getByText } = render(
      <DeactivateUserModal
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={true}
      />,
    );

    expect(getByText("Deactivating...")).toBeInTheDocument();
  });

  it("disables confirm button when isSubmitting is true", () => {
    const { getByText } = render(
      <DeactivateUserModal
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={true}
      />,
    );

    const confirmButton = getByText("Deactivating...").closest("button");
    expect(confirmButton).toBeDisabled();
  });

  it("displays warning message with user details", () => {
    const { getByText } = render(
      <DeactivateUserModal
        username="jane_smith"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(
      getByText(/They will be hidden and removed from your account/),
    ).toBeInTheDocument();
    expect(getByText(/This action cannot be undone/)).toBeInTheDocument();
  });

  it("renders Alert icon container", () => {
    const { container } = render(
      <DeactivateUserModal
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    const iconContainer = container.querySelector("div.bg-intent-critical-10");
    expect(iconContainer).toBeTruthy();
  });

  it("handles long usernames", () => {
    const longUsername = "user_with_a_very_long_username_for_testing";
    const { getByText } = render(
      <DeactivateUserModal
        username={longUsername}
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={false}
      />,
    );

    expect(getByText(new RegExp(longUsername))).toBeInTheDocument();
  });

  it("button is disabled when isSubmitting is true", () => {
    const { getByText } = render(
      <DeactivateUserModal
        username="john_doe"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
        isSubmitting={true}
      />,
    );

    const confirmButton = getByText("Deactivating...").closest("button");
    expect(confirmButton).toBeDisabled();
  });
});
