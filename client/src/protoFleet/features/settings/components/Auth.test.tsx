import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AuthenticationSettings from "./Auth";
import { useAuth } from "@/protoFleet/api/useAuth";
import { useLogin } from "@/protoFleet/api/useLogin";
import { useUsername } from "@/protoFleet/store";

vi.mock("@/protoFleet/api/useAuth");
vi.mock("@/protoFleet/api/useLogin");
vi.mock("@/protoFleet/store");
vi.mock("@/shared/features/toaster");

const mockSetPassword = vi.fn();
const mockUpdatePassword = vi.fn();
const mockUpdateUsername = vi.fn();
const mockLogin = vi.fn();

beforeEach(() => {
  vi.mocked(useAuth).mockReturnValue({
    setPassword: mockSetPassword,
    updatePassword: mockUpdatePassword,
    updateUsername: mockUpdateUsername,
    passwordLastUpdatedAt: null,
  });

  vi.mocked(useLogin).mockReturnValue(mockLogin);
  vi.mocked(useUsername).mockReturnValue("testuser");

  vi.clearAllMocks();
});

describe("AuthenticationSettings", () => {
  describe("autofocus behavior", () => {
    it("autofocuses the current password field in authenticate step", async () => {
      const { getByTestId, getByLabelText } = render(<AuthenticationSettings />);

      // Click Update button for password (second row)
      const passwordRow = getByTestId("password-row");
      const updateButton = passwordRow.querySelector("button");
      if (updateButton) {
        fireEvent.click(updateButton);
      }

      await waitFor(() => {
        const passwordInput = getByLabelText("Password");
        expect(passwordInput).toHaveFocus();
      });
    });

    it("autofocuses the new password field in update password step", async () => {
      mockLogin.mockImplementation(({ onSuccess }) => {
        onSuccess(false);
      });

      const { getByTestId, getByLabelText, getByText } = render(<AuthenticationSettings />);

      // Click Update button for password
      const passwordRow = getByTestId("password-row");
      const updateButton = passwordRow.querySelector("button");
      if (updateButton) {
        fireEvent.click(updateButton);
      }

      // Fill password and submit authenticate step
      await waitFor(() => {
        const passwordInput = getByLabelText("Password");
        fireEvent.change(passwordInput, { target: { value: "currentpass" } });
      });

      const confirmButton = getByText("Confirm");
      fireEvent.click(confirmButton);

      // Check new password field has autofocus
      await waitFor(() => {
        const newPasswordInput = getByLabelText("New password");
        expect(newPasswordInput).toHaveFocus();
      });
    });

    it("autofocuses the new username field in update username step", async () => {
      mockLogin.mockImplementation(({ onSuccess }) => {
        onSuccess(false);
      });

      const { getByTestId, getByLabelText, getByText } = render(<AuthenticationSettings />);

      // Click Update button for username (first row)
      const usernameRow = getByTestId("username-row");
      const updateButton = usernameRow.querySelector("button");
      if (updateButton) {
        fireEvent.click(updateButton);
      }

      // Fill password and submit authenticate step
      await waitFor(() => {
        const passwordInput = getByLabelText("Password");
        fireEvent.change(passwordInput, { target: { value: "currentpass" } });
      });

      const confirmButton = getByText("Confirm");
      fireEvent.click(confirmButton);

      // Check new username field has autofocus
      await waitFor(() => {
        const newUsernameInput = getByLabelText("New username");
        expect(newUsernameInput).toHaveFocus();
      });
    });
  });

  describe("basic rendering", () => {
    it("renders username and password rows", () => {
      const { getByTestId } = render(<AuthenticationSettings />);

      expect(getByTestId("username-row")).toBeInTheDocument();
      expect(getByTestId("password-row")).toBeInTheDocument();
    });

    it("displays current username", () => {
      const { getByTestId } = render(<AuthenticationSettings />);

      const usernameValue = getByTestId("username-value");
      expect(usernameValue).toHaveTextContent("testuser");
    });

    it("opens modal when clicking Update button", () => {
      const { getByText, getByTestId } = render(<AuthenticationSettings />);

      const usernameRow = getByTestId("username-row");
      const updateButton = usernameRow.querySelector("button");
      if (updateButton) {
        fireEvent.click(updateButton);
      }

      expect(getByText("Account password required")).toBeInTheDocument();
    });
  });
});
