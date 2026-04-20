import type { ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AuthenticationSettings from "./Authentication";

const mockNavigate = vi.fn();
const mockLogin = vi.fn();
const mockChangePassword = vi.fn();
const mockUseAccessToken = vi.fn();
const mockSetDefaultPasswordActive = vi.fn();
const mockPushToast = vi.fn<(toast: unknown) => string>(() => "toast-id");
const mockUpdateToast = vi.fn<(toastId: unknown, toast: unknown) => void>();

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mockNavigate,
}));

vi.mock("@/protoOS/api", () => ({
  useLogin: () => mockLogin,
  usePassword: () => ({
    changePassword: mockChangePassword,
  }),
}));

vi.mock("@/protoOS/store", () => ({
  useAccessToken: () => mockUseAccessToken(),
  useSetDefaultPasswordActive: () => mockSetDefaultPasswordActive,
}));

vi.mock("@/shared/features/toaster", () => ({
  STATUSES: {
    loading: "loading",
    success: "success",
    error: "error",
  },
  pushToast: (toast: unknown) => mockPushToast(toast),
  updateToast: (toastId: unknown, toast: unknown) => mockUpdateToast(toastId, toast),
}));

vi.mock("@/shared/components/Setup", () => ({
  Authentication: ({
    submit,
  }: {
    submit: (currentPassword: string, newPassword: string) => void;
    isUpdateMode?: boolean;
    isSubmitting?: boolean;
    setIsSubmitting?: (isSubmitting: boolean) => void;
    headline?: ReactNode;
    description?: ReactNode;
    initUsername?: string;
  }) => <button onClick={() => submit("current-pass", "new-pass")}>Submit</button>,
}));

describe("AuthenticationSettings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseAccessToken.mockReturnValue({ hasAccess: true });
  });

  it("clears defaultPasswordActive after the follow-up login succeeds", async () => {
    mockChangePassword.mockImplementation(({ onSuccess }) => {
      onSuccess?.();
    });
    mockLogin.mockImplementation(({ onSuccess }) => {
      onSuccess?.("access-token", "refresh-token");
    });

    render(<AuthenticationSettings />);

    fireEvent.click(screen.getByText("Submit"));

    await waitFor(() => {
      expect(mockSetDefaultPasswordActive).toHaveBeenCalledWith(false);
      expect(mockNavigate).toHaveBeenCalledWith("/");
    });
  });
});
