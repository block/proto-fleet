import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";
import AccountInvitation from "./AccountInvitation";
import { hostedAccountsClient } from "./api";
import { authClient } from "@/protoFleet/api/clients";
import { AuthenticateResponseSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import {
  PreviewInvitationResponseSchema,
  RegisterInvitedUserResponseSchema,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";
const initialAuth = useFleetStore.getState().auth;
const accountId = "11111111-1111-4111-8111-111111111111";
const token = "x".repeat(43);
afterEach(() => {
  vi.restoreAllMocks();
  useFleetStore.setState({ auth: initialAuth });
});
const LocationReader = () => <span data-testid="location">{useLocation().hash}</span>;
describe("invited login lifecycle", () => {
  it.each(["success", "failure"])("ignores an abandoned login's late %s after registration", async (outcome) => {
    vi.spyOn(hostedAccountsClient, "previewInvitation").mockResolvedValue(
      create(PreviewInvitationResponseSchema, { accountName: "Customer" }),
    );
    vi.spyOn(hostedAccountsClient, "registerInvitedUser").mockResolvedValue(
      create(RegisterInvitedUserResponseSchema, { accountId }),
    );
    let resolve!: (value: ReturnType<typeof create<typeof AuthenticateResponseSchema>>) => void;
    let reject!: (error: Error) => void;
    let abandonedSignal!: AbortSignal;
    vi.spyOn(authClient, "authenticate")
      .mockImplementationOnce((_request, options) => {
        abandonedSignal = options!.signal!;
        return new Promise((r, j) => {
          resolve = r;
          reject = j;
        });
      })
      .mockResolvedValueOnce(
        create(AuthenticateResponseSchema, {
          sessionExpiry: 4000000000n,
          userInfo: { username: "new_user", permissions: [] },
        }),
      );
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={[`/account-invitation#token=${token}`]}>
        <LocationReader />
        <Routes>
          <Route path="/account-invitation" element={<AccountInvitation />} />
          <Route path="/accounts/:accountId/overview" element={<div>Joined account</div>} />
        </Routes>
      </MemoryRouter>,
    );
    await user.type(await screen.findByLabelText("Proto Fleet username"), "old_user");
    await user.type(screen.getByLabelText("Proto Fleet password"), "old-test-password");
    await user.click(screen.getByRole("button", { name: "Log in" }));
    await user.click(screen.getByRole("button", { name: "Create login" }));
    expect(abandonedSignal.aborted).toBe(true);
    expect(screen.getByTestId("location")).toBeEmptyDOMElement();
    await user.type(screen.getByLabelText("Username"), "new_user");
    await user.type(screen.getByLabelText("Choose a password"), "new-test-password");
    await user.click(screen.getByRole("button", { name: "Create login and join" }));
    await screen.findByText("Joined account");
    await act(async () => {
      if (outcome === "success")
        resolve(
          create(AuthenticateResponseSchema, {
            sessionExpiry: 4000000000n,
            userInfo: { username: "old_user", permissions: ["miner:read"] },
          }),
        );
      else reject(new ConnectError("Login rejected", Code.Unauthenticated));
    });
    expect(useFleetStore.getState().auth.username).toBe("new_user");
    expect(useFleetStore.getState().auth.isAuthenticated).toBe(true);
    expect(screen.getByText("Joined account")).toBeInTheDocument();
    expect(hostedAccountsClient.registerInvitedUser).toHaveBeenCalledWith(
      { token, username: "new_user", password: "new-test-password" },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });
});
