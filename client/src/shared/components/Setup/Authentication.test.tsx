import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Authentication from "./Authentication";

describe("Authentication", () => {
  const mockSubmit = vi.fn();
  const mockSetIsSubmitting = vi.fn();

  const defaultProps = {
    headline: "Create your account",
    submit: mockSubmit,
    isSubmitting: false,
    setIsSubmitting: mockSetIsSubmitting,
  };

  describe("autofocus behavior", () => {
    it("autofocuses the username input when no initUsername is provided", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} />);

      const usernameInput = getByLabelText("Username");
      expect(usernameInput).toHaveFocus();
    });

    it("does not autofocus the username input when initUsername is provided", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} initUsername="testuser" />);

      const usernameInput = getByLabelText("Username");
      expect(usernameInput).not.toHaveFocus();
    });

    it("autofocuses the password input when initUsername is provided and not in update mode", () => {
      const { getByLabelText } = render(
        <Authentication {...defaultProps} initUsername="testuser" isUpdateMode={false} />,
      );

      const passwordInput = getByLabelText("Password");
      expect(passwordInput).toHaveFocus();
    });

    it("autofocuses the current password input in update mode when initUsername is provided", () => {
      const { getByLabelText } = render(
        <Authentication {...defaultProps} initUsername="testuser" isUpdateMode={true} />,
      );

      const currentPasswordInput = getByLabelText("Current password");
      expect(currentPasswordInput).toHaveFocus();
    });

    it("does not autofocus the current password input in update mode when no initUsername is provided", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} isUpdateMode={true} />);

      const currentPasswordInput = getByLabelText("Current password");
      expect(currentPasswordInput).not.toHaveFocus();
    });

    it("does not autofocus the password input when no initUsername and not in update mode", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} isUpdateMode={false} />);

      const passwordInput = getByLabelText("Password");
      expect(passwordInput).not.toHaveFocus();
    });
  });

  describe("basic rendering", () => {
    it("renders username and password inputs", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} />);

      expect(getByLabelText("Username")).toBeInTheDocument();
      expect(getByLabelText("Password")).toBeInTheDocument();
    });

    it("renders confirm password input when requirePasswordConfirmation is true", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} requirePasswordConfirmation={true} />);

      expect(getByLabelText("Confirm password")).toBeInTheDocument();
    });

    it("does not render confirm password input when requirePasswordConfirmation is false", () => {
      const { queryByLabelText } = render(<Authentication {...defaultProps} requirePasswordConfirmation={false} />);

      expect(queryByLabelText("Confirm password")).not.toBeInTheDocument();
    });

    it("renders current password input in update mode", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} isUpdateMode={true} />);

      expect(getByLabelText("Current password")).toBeInTheDocument();
    });

    it("disables username input when initUsername is provided", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} initUsername="testuser" />);

      const usernameInput = getByLabelText("Username");
      expect(usernameInput).toBeDisabled();
      expect(usernameInput).toHaveValue("testuser");
    });

    it("renders Continue button", () => {
      const { getByText } = render(<Authentication {...defaultProps} />);

      expect(getByText("Continue")).toBeInTheDocument();
    });

    it("allows input values to be changed", () => {
      const { getByLabelText } = render(<Authentication {...defaultProps} />);

      const usernameInput = getByLabelText("Username");
      fireEvent.change(usernameInput, { target: { value: "newuser" } });

      expect(usernameInput).toHaveValue("newuser");
    });
  });
});
