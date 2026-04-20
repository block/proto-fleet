import React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import UpdateMinerPasswordModal from "./UpdateMinerPasswordModal";

// Mock the Setup components
vi.mock("@/shared/components/Setup", () => ({
  PasswordStrengthMeter: vi.fn(({ onSetScore, password }) => {
    // Simulate password strength scoring
    React.useEffect(() => {
      if (password) {
        const score = password.length >= 12 ? 60 : password.length >= 8 ? 40 : 0;
        onSetScore(score);
      }
    }, [password, onSetScore]);
    return <div data-testid="password-strength-meter">Strength: {password.length >= 12 ? "Strong" : "Weak"}</div>;
  }),
  WeakPasswordWarning: vi.fn(({ onReturn, onContinue }) => (
    <div data-testid="weak-password-warning">
      <button onClick={onReturn}>Create a stronger password</button>
      <button onClick={onContinue}>Continue anyway</button>
    </div>
  )),
}));

// Mock Modal component
vi.mock("@/shared/components/Modal/Modal", () => ({
  default: vi.fn(({ open, children, buttons, onDismiss }) => {
    if (!open) return null;
    return (
      <div data-testid="update-password-modal">
        {children}
        <div data-testid="modal-buttons">
          {buttons?.map((button: { text: string; onClick: () => void; disabled?: boolean }, index: number) => (
            <button
              key={index}
              onClick={button.onClick}
              disabled={button.disabled}
              data-testid={`modal-button-${index}`}
            >
              {button.text}
            </button>
          ))}
        </div>
        <button onClick={onDismiss} data-testid="modal-dismiss">
          Dismiss
        </button>
      </div>
    );
  }),
}));

// Mock Input component
vi.mock("@/shared/components/Input", () => ({
  default: vi.fn(({ id, label, type, onChange, autoFocus }) => (
    <div>
      <label htmlFor={id}>{label}</label>
      <input id={id} type={type} onChange={(e) => onChange(e.target.value)} autoFocus={autoFocus} data-testid={id} />
    </div>
  )),
}));

describe("UpdateMinerPasswordModal", () => {
  const mockOnConfirm = vi.fn();
  const mockOnDismiss = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("Rendering", () => {
    test("renders modal when open is true", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      expect(screen.getByTestId("update-password-modal")).toBeInTheDocument();
      expect(screen.getByTestId("currentPassword")).toBeInTheDocument();
      expect(screen.getByTestId("newPassword")).toBeInTheDocument();
      expect(screen.getByTestId("confirmPassword")).toBeInTheDocument();
    });

    test("does not render modal when open is false", () => {
      render(
        <UpdateMinerPasswordModal
          open={false}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      expect(screen.queryByTestId("update-password-modal")).not.toBeInTheDocument();
    });

    test("renders password strength meter for Proto rigs", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const newPasswordInput = screen.getByTestId("newPassword");
      fireEvent.change(newPasswordInput, { target: { value: "TestPassword123" } });

      expect(screen.getByTestId("password-strength-meter")).toBeInTheDocument();
    });

    test("does not render password strength meter for third-party miners", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={true}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      expect(screen.queryByTestId("password-strength-meter")).not.toBeInTheDocument();
    });

    test("autofocuses the current password input", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      expect(currentPasswordInput).toHaveFocus();
    });
  });

  describe("Validation - Proto Rigs", () => {
    test("button is disabled when current password is empty", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(newPasswordInput, { target: { value: "NewPassword123" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "NewPassword123" } });

      expect(continueButton).toBeDisabled();
      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    test("button is disabled when new password is empty", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "NewPassword123" } });

      expect(continueButton).toBeDisabled();
      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    test("button is disabled when confirm password is empty", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "NewPassword123" } });

      expect(continueButton).toBeDisabled();
      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    test("shows validation error when passwords do not match", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "NewPassword123" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "DifferentPassword123" } });
      fireEvent.click(continueButton);

      expect(screen.getByText("Passwords don't match")).toBeInTheDocument();
      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    test("shows validation error when password is too short (Proto rigs only)", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "short" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "short" } });
      fireEvent.click(continueButton);

      expect(screen.getByText("Minimum 8 characters required")).toBeInTheDocument();
      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    test("shows weak password warning for Proto rigs with weak password", async () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "weakpass" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "weakpass" } });

      await waitFor(() => {
        fireEvent.click(continueButton);
      });

      await waitFor(() => {
        expect(screen.getByTestId("weak-password-warning")).toBeInTheDocument();
      });
      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    test("calls onConfirm when user continues with weak password", async () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "weakpass" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "weakpass" } });
      fireEvent.click(continueButton);

      await waitFor(() => {
        expect(screen.getByTestId("weak-password-warning")).toBeInTheDocument();
      });

      const continueAnywayButton = screen.getByText("Continue anyway");
      fireEvent.click(continueAnywayButton);

      await waitFor(() => {
        expect(mockOnConfirm).toHaveBeenCalledWith("CurrentPassword123", "weakpass");
      });
    });

    test("returns to main modal when user clicks 'Create a stronger password'", async () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "weakpass" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "weakpass" } });
      fireEvent.click(continueButton);

      await waitFor(() => {
        expect(screen.getByTestId("weak-password-warning")).toBeInTheDocument();
      });

      const createStrongerButton = screen.getByText("Create a stronger password");
      fireEvent.click(createStrongerButton);

      await waitFor(() => {
        expect(screen.getByTestId("update-password-modal")).toBeInTheDocument();
        expect(screen.queryByTestId("weak-password-warning")).not.toBeInTheDocument();
      });
    });
  });

  describe("Validation - Third-Party Miners", () => {
    test("does not validate password length for third-party miners", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={true}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "current" } });
      fireEvent.change(newPasswordInput, { target: { value: "abc" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "abc" } });
      fireEvent.click(continueButton);

      expect(mockOnConfirm).toHaveBeenCalledWith("current", "abc");
    });

    test("does not show weak password warning for third-party miners", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={true}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "weak" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "weak" } });
      fireEvent.click(continueButton);

      expect(screen.queryByTestId("weak-password-warning")).not.toBeInTheDocument();
      expect(mockOnConfirm).toHaveBeenCalledWith("CurrentPassword123", "weak");
    });
  });

  describe("Successful submission", () => {
    test("calls onConfirm with correct parameters for Proto rigs with strong password", async () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");
      const continueButton = screen.getByTestId("modal-button-0");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "StrongPassword123456" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "StrongPassword123456" } });

      await waitFor(() => {
        fireEvent.click(continueButton);
      });

      await waitFor(() => {
        expect(mockOnConfirm).toHaveBeenCalledWith("CurrentPassword123", "StrongPassword123456");
      });
    });
  });

  describe("Enter key handling", () => {
    test("submits form when Enter key is pressed with valid inputs", async () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "StrongPassword123456" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "StrongPassword123456" } });

      await waitFor(() => {
        fireEvent.keyDown(confirmPasswordInput, { key: "Enter", code: "Enter" });
      });

      await waitFor(() => {
        expect(mockOnConfirm).toHaveBeenCalledWith("CurrentPassword123", "StrongPassword123456");
      });
    });

    test("does not submit form when Enter key is pressed with empty fields", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");

      fireEvent.keyDown(currentPasswordInput, { key: "Enter", code: "Enter" });

      expect(mockOnConfirm).not.toHaveBeenCalled();
    });
  });

  describe("Form reset", () => {
    test("resets form when modal is dismissed and reopened", async () => {
      const { rerender } = render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword") as HTMLInputElement;
      const newPasswordInput = screen.getByTestId("newPassword") as HTMLInputElement;

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "NewPassword123" } });

      expect(currentPasswordInput.value).toBe("CurrentPassword123");
      expect(newPasswordInput.value).toBe("NewPassword123");

      // Close modal
      rerender(
        <UpdateMinerPasswordModal
          open={false}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      // Reopen modal
      rerender(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInputAfter = screen.getByTestId("currentPassword") as HTMLInputElement;
      const newPasswordInputAfter = screen.getByTestId("newPassword") as HTMLInputElement;

      await waitFor(() => {
        expect(currentPasswordInputAfter.value).toBe("");
        expect(newPasswordInputAfter.value).toBe("");
      });
    });
  });

  describe("Dismiss handling", () => {
    test("calls onDismiss when dismiss button is clicked", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const dismissButton = screen.getByTestId("modal-dismiss");
      fireEvent.click(dismissButton);

      expect(mockOnDismiss).toHaveBeenCalled();
    });
  });

  describe("Button states", () => {
    test("disables Continue button when fields are empty", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const continueButton = screen.getByTestId("modal-button-0");
      expect(continueButton).toBeDisabled();
    });

    test("enables Continue button when all fields are filled", () => {
      render(
        <UpdateMinerPasswordModal
          open={true}
          hasThirdPartyMiners={false}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const currentPasswordInput = screen.getByTestId("currentPassword");
      const newPasswordInput = screen.getByTestId("newPassword");
      const confirmPasswordInput = screen.getByTestId("confirmPassword");

      fireEvent.change(currentPasswordInput, { target: { value: "CurrentPassword123" } });
      fireEvent.change(newPasswordInput, { target: { value: "NewPassword123" } });
      fireEvent.change(confirmPasswordInput, { target: { value: "NewPassword123" } });

      const continueButton = screen.getByTestId("modal-button-0");
      expect(continueButton).not.toBeDisabled();
    });
  });
});
