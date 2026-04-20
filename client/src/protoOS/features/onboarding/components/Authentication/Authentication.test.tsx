import type { ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AuthenticationPage from "./Authentication";

const mockNavigate = vi.fn();
const mockLogin = vi.fn();
const mockSetPassword = vi.fn();
const mockChangePassword = vi.fn();
const mockUsePasswordSet = vi.fn();
const mockUseDefaultPasswordActive = vi.fn();
const mockSetDefaultPasswordActive = vi.fn();

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mockNavigate,
}));

vi.mock("@/protoOS/api", () => ({
  useLogin: () => mockLogin,
  usePassword: () => ({
    setPassword: mockSetPassword,
    changePassword: mockChangePassword,
  }),
}));

vi.mock("@/protoOS/store", () => ({
  usePasswordSet: () => mockUsePasswordSet(),
  useDefaultPasswordActive: () => mockUseDefaultPasswordActive(),
  useSetDefaultPasswordActive: () => mockSetDefaultPasswordActive,
}));

vi.mock("@/shared/components/Setup", () => ({
  Authentication: ({
    headline,
    isUpdateMode,
    submit,
  }: {
    headline: ReactNode;
    isUpdateMode?: boolean;
    submit: (first: string, second: string) => void;
  }) => (
    <div>
      <div>{headline}</div>
      <div data-testid="update-mode">{String(Boolean(isUpdateMode))}</div>
      <button onClick={() => submit(isUpdateMode ? "current-pass" : "new-pass", isUpdateMode ? "new-pass" : "admin")}>
        Submit
      </button>
    </div>
  ),
  OnboardingLayout: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

describe("AuthenticationPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUsePasswordSet.mockReturnValue(false);
    mockUseDefaultPasswordActive.mockReturnValue(false);
  });

  it("navigates to mining pool when password is already set and default password is inactive", async () => {
    mockUsePasswordSet.mockReturnValue(true);
    mockUseDefaultPasswordActive.mockReturnValue(false);

    render(<AuthenticationPage />);

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith("/onboarding/mining-pool");
    });
  });

  it("stays on the page and switches to update mode when the default password is active", () => {
    mockUsePasswordSet.mockReturnValue(true);
    mockUseDefaultPasswordActive.mockReturnValue(true);

    render(<AuthenticationPage />);

    expect(mockNavigate).not.toHaveBeenCalled();
    expect(screen.getByText("Update your admin login")).toBeInTheDocument();
    expect(screen.getByTestId("update-mode")).toHaveTextContent("true");
  });

  it("treats a missing default-password flag like false for older firmware", async () => {
    mockUsePasswordSet.mockReturnValue(true);
    mockUseDefaultPasswordActive.mockReturnValue(undefined);

    render(<AuthenticationPage />);

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith("/onboarding/mining-pool");
    });
  });

  it("logs in with the current password, changes it, then logs in with the new password", async () => {
    mockUsePasswordSet.mockReturnValue(true);
    mockUseDefaultPasswordActive.mockReturnValue(true);
    mockLogin.mockImplementation(({ onSuccess }) => {
      onSuccess?.("access-token", "refresh-token");
    });
    mockChangePassword.mockImplementation(({ onSuccess }) => {
      onSuccess?.();
    });

    render(<AuthenticationPage />);

    fireEvent.click(screen.getByText("Submit"));

    await waitFor(() => {
      expect(mockLogin).toHaveBeenNthCalledWith(
        1,
        expect.objectContaining({
          password: "current-pass",
          onError: expect.any(Function),
          onSuccess: expect.any(Function),
        }),
      );
      expect(mockChangePassword).toHaveBeenCalledWith(
        expect.objectContaining({
          changePasswordRequest: {
            current_password: "current-pass",
            new_password: "new-pass",
          },
          onError: expect.any(Function),
          onSuccess: expect.any(Function),
        }),
      );
      expect(mockLogin).toHaveBeenNthCalledWith(
        2,
        expect.objectContaining({
          password: "new-pass",
          onError: expect.any(Function),
          onSuccess: expect.any(Function),
        }),
      );
      expect(mockSetDefaultPasswordActive).toHaveBeenCalledWith(false);
      expect(mockNavigate).toHaveBeenCalledWith("/onboarding/mining-pool");
    });
  });

  it("does not clear defaultPasswordActive before the follow-up login succeeds", async () => {
    mockUsePasswordSet.mockReturnValue(true);
    mockUseDefaultPasswordActive.mockReturnValue(true);
    mockLogin
      .mockImplementationOnce(({ onSuccess }) => {
        onSuccess?.("access-token", "refresh-token");
      })
      .mockImplementationOnce(() => {});
    mockChangePassword.mockImplementation(({ onSuccess }) => {
      onSuccess?.();
    });

    render(<AuthenticationPage />);

    fireEvent.click(screen.getByText("Submit"));

    await waitFor(() => {
      expect(mockChangePassword).toHaveBeenCalled();
      expect(mockLogin).toHaveBeenCalledTimes(2);
    });

    expect(mockSetDefaultPasswordActive).not.toHaveBeenCalled();
    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
