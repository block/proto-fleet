import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ResetPasswordModal from "./ResetPasswordModal";

vi.mock("@/shared/features/toaster");

const mockOnConfirm = vi.fn();
const mockOnDismiss = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("ResetPasswordModal", () => {
  describe("Step 1: Confirmation", () => {
    it("renders confirmation step when temporaryPassword is null", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword={null}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      expect(getByText("Reset member password?")).toBeInTheDocument();
      expect(getByText("Cancel")).toBeInTheDocument();
      expect(getByText("Reset member password")).toBeInTheDocument();
    });

    it("calls onConfirm when Reset member password is clicked", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword={null}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      fireEvent.click(getByText("Reset member password"));
      expect(mockOnConfirm).toHaveBeenCalled();
    });

    it("calls onDismiss when Cancel is clicked", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword={null}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      fireEvent.click(getByText("Cancel"));
      expect(mockOnDismiss).toHaveBeenCalled();
    });

    it("shows loading state when isResetting is true", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword={null}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={true}
        />,
      );

      const resetButton = getByText("Reset member password").closest("button");
      expect(resetButton).toHaveAttribute("aria-busy", "true");
    });

    it("disables button when isResetting is true", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword={null}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={true}
        />,
      );

      const resetButton = getByText("Reset member password").closest("button");
      expect(resetButton).toBeDisabled();
    });

    it("renders Lock icon container in confirmation step", () => {
      const { container } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword={null}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      const iconContainer = container.querySelector(
        "div.bg-surface-elevated-base",
      );
      expect(iconContainer).toBeTruthy();
    });
  });

  describe("Step 2: Success with password", () => {
    it("renders success step when temporaryPassword is provided", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword="TempPass123!@#"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      expect(getByText("Password reset")).toBeInTheDocument();
      expect(getByText("TempPass123!@#")).toBeInTheDocument();
      expect(getByText("Done")).toBeInTheDocument();
    });

    it("displays username in success message", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="jane_smith"
          temporaryPassword="TempPass456"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      expect(
        getByText(/jane_smith's password has been reset/),
      ).toBeInTheDocument();
    });

    it("calls onDismiss when Done is clicked", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword="TempPass123!@#"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      fireEvent.click(getByText("Done"));
      expect(mockOnDismiss).toHaveBeenCalled();
    });

    it("allows copying temporary password", () => {
      Object.assign(navigator, {
        clipboard: {
          writeText: vi.fn().mockResolvedValue(undefined),
        },
      });

      const { getByRole } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword="TempPass123!@#"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      const copyButton = getByRole("button", { name: /copy password/i });
      fireEvent.click(copyButton);

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
        "TempPass123!@#",
      );
    });

    it("renders Success icon container in success step", () => {
      const { container } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword="TempPass123!@#"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      const iconContainer = container.querySelector("div.bg-intent-success-10");
      expect(iconContainer).toBeTruthy();
    });

    it("displays temporary password in monospace font", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword="TempPass123!@#"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      const passwordElement = getByText("TempPass123!@#");
      expect(passwordElement.className).toContain("font-mono");
    });

    it("handles long usernames in success message", () => {
      const longUsername = "user_with_a_very_long_username_for_testing";
      const { getByText } = render(
        <ResetPasswordModal
          username={longUsername}
          temporaryPassword="TempPass123!@#"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      expect(
        getByText(new RegExp(`${longUsername}'s password has been reset`)),
      ).toBeInTheDocument();
    });
  });

  describe("Modal dismissal", () => {
    it("only calls onDismiss when clicking Done in success step", () => {
      const { getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword="TempPass123!@#"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      fireEvent.click(getByText("Done"));
      expect(mockOnDismiss).toHaveBeenCalled();
      expect(mockOnConfirm).not.toHaveBeenCalled();
    });
  });
});
