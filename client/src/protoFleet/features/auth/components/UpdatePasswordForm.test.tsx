import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UpdatePasswordForm } from "./UpdatePasswordForm";

const mockOnSubmit = vi.fn();
const mockOnErrorDismiss = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("UpdatePasswordForm", () => {
  it("renders form with password inputs", () => {
    const { getByLabelText, getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    expect(getByText("Update Your Password")).toBeInTheDocument();
    expect(getByLabelText("New password")).toBeInTheDocument();
    expect(getByLabelText("Confirm password")).toBeInTheDocument();
    expect(getByText("Continue")).toBeInTheDocument();
  });

  it("allows entering password values", () => {
    const { getByLabelText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    const newPasswordInput = getByLabelText("New password");
    const confirmPasswordInput = getByLabelText("Confirm password");

    fireEvent.change(newPasswordInput, { target: { value: "NewPass123!@#" } });
    fireEvent.change(confirmPasswordInput, {
      target: { value: "NewPass123!@#" },
    });

    expect(newPasswordInput).toHaveValue("NewPass123!@#");
    expect(confirmPasswordInput).toHaveValue("NewPass123!@#");
  });

  it("calls onSubmit with password values when Continue is clicked", () => {
    const { getByLabelText, getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    const newPasswordInput = getByLabelText("New password");
    const confirmPasswordInput = getByLabelText("Confirm password");

    fireEvent.change(newPasswordInput, { target: { value: "NewPass123" } });
    fireEvent.change(confirmPasswordInput, { target: { value: "NewPass123" } });

    fireEvent.click(getByText("Continue"));

    expect(mockOnSubmit).toHaveBeenCalledWith("NewPass123", "NewPass123");
  });

  it("displays error message when provided", () => {
    const { getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} errorMsg="Passwords do not match" />);

    expect(getByText("Passwords do not match")).toBeInTheDocument();
  });

  it("calls onErrorDismiss when typing in new password field", () => {
    const { getByLabelText } = render(
      <UpdatePasswordForm
        onSubmit={mockOnSubmit}
        errorMsg="Passwords do not match"
        onErrorDismiss={mockOnErrorDismiss}
      />,
    );

    const newPasswordInput = getByLabelText("New password");
    fireEvent.change(newPasswordInput, { target: { value: "test" } });

    expect(mockOnErrorDismiss).toHaveBeenCalled();
  });

  it("calls onErrorDismiss when typing in confirm password field", () => {
    const { getByLabelText } = render(
      <UpdatePasswordForm
        onSubmit={mockOnSubmit}
        errorMsg="Passwords do not match"
        onErrorDismiss={mockOnErrorDismiss}
      />,
    );

    const confirmPasswordInput = getByLabelText("Confirm password");
    fireEvent.change(confirmPasswordInput, { target: { value: "test" } });

    expect(mockOnErrorDismiss).toHaveBeenCalled();
  });

  it("shows loading state when isSubmitting is true", () => {
    const { getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} isSubmitting={true} />);

    expect(getByText("Updating...")).toBeInTheDocument();
  });

  it("disables button when isSubmitting is true", () => {
    const { getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} isSubmitting={true} />);

    const continueButton = getByText("Updating...").closest("button");
    expect(continueButton).toBeDisabled();
  });

  it("renders password strength meter", () => {
    const { getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    expect(getByText("Password strength")).toBeInTheDocument();
  });

  it("renders Logo component", () => {
    const { container } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    const logo = container.querySelector("svg");
    expect(logo).toBeTruthy();
  });

  it("renders Footer component", () => {
    const { container } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    expect(container.querySelector("footer")).toBeTruthy();
  });

  it("displays error message with correct styling", () => {
    const { getByText, getByTestId } = render(
      <UpdatePasswordForm onSubmit={mockOnSubmit} errorMsg="Invalid password format" />,
    );

    expect(getByText("Invalid password format")).toBeInTheDocument();
    expect(getByTestId("callout")).toBeInTheDocument();
  });

  it("shows validation error when submitting with empty passwords", () => {
    const { getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    fireEvent.click(getByText("Continue"));

    expect(mockOnSubmit).not.toHaveBeenCalled();
    expect(getByText("Minimum 8 characters required")).toBeInTheDocument();
  });

  it("updates password strength meter when password changes", () => {
    const { getByLabelText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    const newPasswordInput = getByLabelText("New password");

    fireEvent.change(newPasswordInput, { target: { value: "weak" } });

    fireEvent.change(newPasswordInput, {
      target: { value: "StrongP@ssw0rd123!" },
    });

    expect(newPasswordInput).toHaveValue("StrongP@ssw0rd123!");
  });

  it("handles long error messages", () => {
    const longError =
      "Password must be at least 12 characters long and include uppercase letters, lowercase letters, numbers, and special characters. Please try again.";

    const { getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} errorMsg={longError} />);

    expect(getByText(longError)).toBeInTheDocument();
  });

  it("does not show error message when errorMsg is empty string", () => {
    const { queryByTestId } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} errorMsg="" />);

    expect(queryByTestId("callout")).toBeFalsy();
  });

  it("renders descriptive text about temporary password", () => {
    const { getByText } = render(<UpdatePasswordForm onSubmit={mockOnSubmit} />);

    expect(
      getByText("You logged in with a temporary password. Enter your new password to continue."),
    ).toBeInTheDocument();
  });
});
