import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { authClient } from "./clients";
import { AuthenticateRequestSchema, AuthenticateResponseSchema } from "./generated/auth/v1/auth_pb";
import { useLogin } from "./useLogin";
import { useFleetStore } from "@/protoFleet/store";
const initialAuth = useFleetStore.getState().auth;
afterEach(() => {
  vi.restoreAllMocks();
  useFleetStore.setState({ auth: initialAuth });
});
describe("page-owned authentication", () => {
  it("does not restore login or call callbacks after its request was cancelled", async () => {
    let resolve!: (response: ReturnType<typeof create<typeof AuthenticateResponseSchema>>) => void;
    vi.spyOn(authClient, "authenticate").mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    const controller = new AbortController(),
      onSuccess = vi.fn(),
      onFinally = vi.fn();
    const { result } = renderHook(() => useLogin());
    let pending!: Promise<void>;
    act(() => {
      pending = result.current({
        loginRequest: create(AuthenticateRequestSchema, { username: "new_user", password: "test-password" }),
        signal: controller.signal,
        onSuccess,
        onFinally,
      });
    });
    controller.abort();
    await act(async () => {
      resolve(create(AuthenticateResponseSchema, { sessionExpiry: 4000000000n, userInfo: { username: "new_user" } }));
      await pending;
    });
    expect(useFleetStore.getState().auth).toEqual(initialAuth);
    expect(onSuccess).not.toHaveBeenCalled();
    expect(onFinally).not.toHaveBeenCalled();
  });
});
