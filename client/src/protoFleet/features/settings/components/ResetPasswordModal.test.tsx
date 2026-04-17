import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ResetPasswordModal from "./ResetPasswordModal";
import * as utility from "@/shared/utils/utility";

vi.mock("@/shared/features/toaster");
vi.mock("@/shared/utils/utility", async () => {
  const actual = await vi.importActual<typeof utility>("@/shared/utils/utility");
  return {
    ...actual,
    copyToClipboard: vi.fn().mockResolvedValue(undefined),
  };
});

const mockOnConfirm = vi.fn();
const mockOnDismiss = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("ResetPasswordModal", () => {
  describe("Step 1: Confirmation", () => {
    it("renders confirmation step when temporaryPassword is null", () => {
      const { getByTestId, getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword={null}
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      expect(getByTestId("modal")).toBeInTheDocument();
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
  });

  describe("Step 2: Success with password", () => {
    it("renders success step when temporaryPassword is provided", () => {
      const { getByTestId, getByText } = render(
        <ResetPasswordModal
          username="john_doe"
          temporaryPassword="TempPass123!@#"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
          isResetting={false}
        />,
      );

      expect(getByTestId("modal")).toBeInTheDocument();
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

      expect(getByText(/jane_smith's password has been reset/)).toBeInTheDocument();
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

      expect(utility.copyToClipboard).toHaveBeenCalledWith("TempPass123!@#");
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

      expect(getByText(new RegExp(`${longUsername}'s password has been reset`))).toBeInTheDocument();
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
