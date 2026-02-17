import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { usePassword } from "./usePassword";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext/useMinerHosting";

vi.mock("@/protoOS/contexts/MinerHostingContext/useMinerHosting", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useAuthErrors: vi.fn(),
  useAuthHeader: vi.fn(),
  useSetPasswordSet: vi.fn(),
}));

describe("usePassword", () => {
  const mockChangePassword = vi.fn();
  const mockSetPassword = vi.fn();
  const mockHandleAuthErrors = vi.fn();
  const mockSetPasswordSet = vi.fn();
  const mockAuthHeader = { headers: { Authorization: "Bearer test-token" } };

  beforeEach(async () => {
    vi.clearAllMocks();

    (useMinerHosting as Mock).mockReturnValue({
      api: {
        changePassword: mockChangePassword,
        setPassword: mockSetPassword,
      },
    });

    const mockStore = await import("@/protoOS/store");
    (mockStore.useAuthErrors as Mock).mockReturnValue({
      handleAuthErrors: mockHandleAuthErrors,
    });
    (mockStore.useAuthHeader as Mock).mockReturnValue(mockAuthHeader);
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

      await waitFor(() => {
        expect(mockChangePassword).toHaveBeenCalledWith(changePasswordRequest, mockAuthHeader);
        expect(onSuccess).toHaveBeenCalledTimes(1);
        expect(onFinally).toHaveBeenCalledTimes(1);
      });
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

      await waitFor(() => {
        expect(onError).toHaveBeenCalledWith("Password verification error: VerifyingPasswordFailed");
        expect(onSuccess).not.toHaveBeenCalled();
        expect(mockHandleAuthErrors).not.toHaveBeenCalled();
        expect(mockChangePassword).toHaveBeenCalledTimes(1);
        expect(onFinally).toHaveBeenCalledTimes(1);
      });
    });

    test("attempts token refresh for non-password 401 errors", async () => {
      const error = { status: 401, error: { message: "Error validating JWT token: ExpiredSignature" } };
      mockChangePassword.mockRejectedValue(error);

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest });

      await waitFor(() => {
        expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
        expect(mockHandleAuthErrors).toHaveBeenCalledWith({
          error,
          onError: expect.any(Function),
          onSuccess: expect.any(Function),
        });
      });
    });

    test("retries with fresh access token after token refresh", async () => {
      const tokenError = { status: 401, error: { message: "Error validating JWT token: ExpiredSignature" } };
      mockChangePassword.mockRejectedValueOnce(tokenError).mockResolvedValueOnce(undefined);
      const onSuccess = vi.fn();
      const onError = vi.fn();
      const onFinally = vi.fn();
      const freshToken = "fresh-access-token";

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest, onSuccess, onError, onFinally });

      await waitFor(() => {
        expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
      });

      // Simulate successful token refresh passing the new access token
      const authErrorCall = mockHandleAuthErrors.mock.calls[0][0];
      await authErrorCall.onSuccess(freshToken);

      await waitFor(() => {
        expect(mockChangePassword).toHaveBeenCalledTimes(2);
        expect(mockChangePassword).toHaveBeenNthCalledWith(1, changePasswordRequest, mockAuthHeader);
        expect(mockChangePassword).toHaveBeenNthCalledWith(2, changePasswordRequest, {
          headers: { Authorization: `Bearer ${freshToken}` },
        });
        expect(onSuccess).toHaveBeenCalledTimes(1);
        expect(onError).not.toHaveBeenCalled();
        expect(onFinally).toHaveBeenCalledTimes(1);
      });
    });

    test("stops retrying after one failed retry (prevents infinite loop)", async () => {
      const tokenError = { status: 401, error: { message: "Error validating JWT token: ExpiredSignature" } };
      mockChangePassword.mockRejectedValue(tokenError);
      const onSuccess = vi.fn();
      const onError = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest, onSuccess, onError, onFinally });

      await waitFor(() => {
        expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
      });

      // Simulate successful token refresh triggering retry
      const authErrorCall = mockHandleAuthErrors.mock.calls[0][0];
      await authErrorCall.onSuccess("fresh-token");

      await waitFor(() => {
        expect(mockChangePassword).toHaveBeenCalledTimes(2);
        expect(onError).toHaveBeenCalledWith("Error validating JWT token: ExpiredSignature");
        expect(onSuccess).not.toHaveBeenCalled();
        expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
        expect(onFinally).toHaveBeenCalledTimes(1);
      });
    });

    test("calls onError and onFinally through handleAuthErrors for non-401 errors", async () => {
      const error500 = { status: 500, error: { message: "Server error" } };
      mockChangePassword.mockRejectedValue(error500);
      const onError = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.changePassword({ changePasswordRequest, onError, onFinally });

      await waitFor(() => {
        expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
      });

      // Simulate handleAuthErrors routing to onError for non-401
      const authErrorCall = mockHandleAuthErrors.mock.calls[0][0];
      authErrorCall.onError();

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

      await waitFor(() => {
        expect(mockSetPassword).toHaveBeenCalledWith({ password });
        expect(mockSetPasswordSet).toHaveBeenCalledWith(true);
        expect(onSuccess).toHaveBeenCalledTimes(1);
        expect(onFinally).toHaveBeenCalledTimes(1);
      });
    });

    test("calls onFinally exactly once after successful retry", async () => {
      const error401 = { status: 401, error: { message: "Unauthorized" } };
      mockSetPassword.mockRejectedValueOnce(error401).mockResolvedValueOnce(undefined);
      const onSuccess = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.setPassword({ password, onSuccess, onFinally });

      await waitFor(() => {
        expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
      });

      // Simulate successful token refresh triggering retry
      const authErrorCall = mockHandleAuthErrors.mock.calls[0][0];
      await authErrorCall.onSuccess();

      await waitFor(() => {
        expect(mockSetPassword).toHaveBeenCalledTimes(2);
        expect(onSuccess).toHaveBeenCalledTimes(1);
        expect(onFinally).toHaveBeenCalledTimes(1);
      });
    });

    test("stops retrying after one failed retry (prevents infinite loop)", async () => {
      const error401 = { status: 401, error: { message: "Unauthorized" } };
      mockSetPassword.mockRejectedValue(error401);
      const onSuccess = vi.fn();
      const onError = vi.fn();
      const onFinally = vi.fn();

      const { result } = renderHook(() => usePassword());

      await result.current.setPassword({ password, onSuccess, onError, onFinally });

      await waitFor(() => {
        expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
      });

      // Simulate successful token refresh triggering retry
      const authErrorCall = mockHandleAuthErrors.mock.calls[0][0];
      await authErrorCall.onSuccess();

      await waitFor(() => {
        expect(mockSetPassword).toHaveBeenCalledTimes(2);
        expect(onError).toHaveBeenCalledWith("Unauthorized");
        expect(onSuccess).not.toHaveBeenCalled();
        expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
        expect(onFinally).toHaveBeenCalledTimes(1);
      });
    });

    test("does not call API if api is not available", () => {
      (useMinerHosting as Mock).mockReturnValue({ api: null });

      const { result } = renderHook(() => usePassword());

      result.current.setPassword({ password });

      expect(mockSetPassword).not.toHaveBeenCalled();
    });
  });
});
