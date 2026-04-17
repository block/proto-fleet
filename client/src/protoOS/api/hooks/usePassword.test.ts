import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { usePassword } from "./usePassword";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext/useMinerHosting";

vi.mock("@/protoOS/contexts/MinerHostingContext/useMinerHosting", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useAuthRetry: vi.fn(),
  useSetDefaultPasswordActive: vi.fn(),
  useSetPasswordSet: vi.fn(),
}));

describe("usePassword", () => {
  const mockChangePassword = vi.fn();
  const mockSetPassword = vi.fn();
  const mockAuthRetry = vi.fn();
  const mockSetPasswordSet = vi.fn();
  const mockSetDefaultPasswordActive = vi.fn();
  const mockAuthHeader = { headers: { Authorization: "Bearer test-token" } };

  beforeEach(async () => {
    vi.clearAllMocks();

    (useMinerHosting as Mock).mockReturnValue({
      api: {
        changePassword: mockChangePassword,
        setPassword: mockSetPassword,
      },
    });

    mockAuthRetry.mockImplementation(async ({ request, onSuccess, onError, shouldRetry }) => {
      try {
        const result = await request(mockAuthHeader);
        await onSuccess?.(result);
      } catch (error) {
        if (shouldRetry && !shouldRetry(error)) {
          onError?.(error);
          return;
        }
        onError?.(error);
      }
    });

    const mockStore = await import("@/protoOS/store");
    (mockStore.useAuthRetry as Mock).mockReturnValue(mockAuthRetry);
    (mockStore.useSetDefaultPasswordActive as Mock).mockReturnValue(mockSetDefaultPasswordActive);
    (mockStore.useSetPasswordSet as Mock).mockReturnValue(mockSetPasswordSet);
  });

  // ===========================================================================
  // changePassword
  // ===========================================================================

  describe("changePassword", () => {
    const changePasswordRequest = { current_password: "old", new_password: "new123" };

    test("calls onSuccess and onFinally on successful password change", async () => {
      mockChangePassword.mockResolvedValue(undefined);
      const onSuccess = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest, onSuccess, onFinally });

      expect(mockChangePassword).toHaveBeenCalledWith(changePasswordRequest, mockAuthHeader);
      expect(mockSetDefaultPasswordActive).not.toHaveBeenCalled();
      expect(onSuccess).toHaveBeenCalledTimes(1);
      expect(onFinally).toHaveBeenCalledTimes(1);
    });

    test("immediately shows error when firmware reports wrong password", async () => {
      const error = {
        status: 401,
        error: { message: "Password verification error: VerifyingPasswordFailed" },
      };
      mockChangePassword.mockRejectedValue(error);
      const onError = vi.fn();
      const onSuccess = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest, onError, onSuccess, onFinally });

      expect(onError).toHaveBeenCalledWith("Password verification error: VerifyingPasswordFailed");
      expect(onSuccess).not.toHaveBeenCalled();
      expect(onFinally).toHaveBeenCalledTimes(1);
    });

    test("passes shouldRetry that permits non-password 401 errors", async () => {
      const tokenError = { status: 401, error: { message: "Error validating JWT token: ExpiredSignature" } };
      mockChangePassword.mockRejectedValue(tokenError);

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest });

      const authRetryCall = mockAuthRetry.mock.calls[0][0];
      expect(authRetryCall.shouldRetry(tokenError)).toBe(true);
    });

    test("passes shouldRetry that blocks password verification errors", async () => {
      const pwError = {
        status: 401,
        error: { message: "Password verification error: VerifyingPasswordFailed" },
      };
      mockChangePassword.mockRejectedValue(pwError);

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest });

      const authRetryCall = mockAuthRetry.mock.calls[0][0];
      expect(authRetryCall.shouldRetry(pwError)).toBe(false);
    });

    test("calls onError with extracted message and onFinally on API error", async () => {
      const error = { status: 500, error: { message: "Server error" } };
      mockChangePassword.mockRejectedValue(error);
      const onError = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest, onError, onFinally });

      expect(onError).toHaveBeenCalledWith("Server error");
      expect(onFinally).toHaveBeenCalledTimes(1);
    });

    test("does not call API if api is not available", () => {
      (useMinerHosting as Mock).mockReturnValue({ api: null });

      const { result } = renderHook(() => usePassword());

      result.current.changePassword({ changePasswordRequest });

      expect(mockChangePassword).not.toHaveBeenCalled();
    });
  });

  // ===========================================================================
  // setPassword
  // ===========================================================================

  describe("setPassword", () => {
    const password = "newpassword123";

    test("calls onSuccess, updates store, and calls onFinally on success", async () => {
      mockSetPassword.mockResolvedValue(undefined);
      const onSuccess = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.setPassword({ password, onSuccess, onFinally });

      expect(mockSetPassword).toHaveBeenCalledWith({ password }, { secure: false });
      expect(mockSetPasswordSet).toHaveBeenCalledWith(true);
      expect(mockSetDefaultPasswordActive).toHaveBeenCalledWith(false);
      expect(onSuccess).toHaveBeenCalledTimes(1);
      expect(onFinally).toHaveBeenCalledTimes(1);
    });

    test("calls onError with extracted message and onFinally on error", async () => {
      const error = { status: 500, error: { message: "Server error" } };
      mockSetPassword.mockRejectedValue(error);
      const onError = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.setPassword({ password, onError, onFinally });

      expect(onError).toHaveBeenCalledWith("Server error");
      expect(onFinally).toHaveBeenCalledTimes(1);
    });

    test("does not call API if api is not available", () => {
      (useMinerHosting as Mock).mockReturnValue({ api: null });

      const { result } = renderHook(() => usePassword());

      result.current.setPassword({ password });

      expect(mockSetPassword).not.toHaveBeenCalled();
    });

    test("awaits authRetry promise before resolving", async () => {
      let resolveRetry!: () => void;
      mockAuthRetry.mockReturnValue(
        new Promise<void>((resolve) => {
          resolveRetry = resolve;
        }),
      );

      const { result } = renderHook(() => usePassword());

      let resolved = false;
      const promise = result.current.setPassword({ password }).then(() => {
        resolved = true;
      });

      await waitFor(() => {
        expect(mockAuthRetry).toHaveBeenCalled();
      });

      expect(resolved).toBe(false);

      resolveRetry();
      await promise;

      expect(resolved).toBe(true);
    });
  });
});
