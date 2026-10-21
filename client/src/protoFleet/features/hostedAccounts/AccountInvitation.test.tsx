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
  AcceptInvitationResponseSchema,
  PreviewInvitationResponseSchema,
  RegisterInvitedUserResponseSchema,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";
const initialAuth = useFleetStore.getState().auth;
const accountId = "11111111-1111-4111-8111-111111111111";
const token = "K7MP-4X9R-6TWC-8NQF";
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
    expect(await screen.findByRole("button", { name: "Create login" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByLabelText("Choose a password")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Cancel" })).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Existing login" }));
    expect(screen.getAllByRole("button", { name: "Cancel" })).toHaveLength(1);
    await user.type(screen.getByLabelText("Proto Fleet username"), "old_user");
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

function openCodeEntry() {
  render(
    <MemoryRouter initialEntries={["/account-invitation"]}>
      <LocationReader />
      <Routes>
        <Route path="/account-invitation" element={<AccountInvitation />} />
        <Route path="/accounts/:accountId/overview" element={<div>Joined account</div>} />
      </Routes>
    </MemoryRouter>,
  );
}
describe("portable invitation codes", () => {
  it.each([token, " k7mp 4x9r 6twc 8nqf "])(
    "removes the invitation link fragment before previewing %j",
    async (code) => {
      const hashes: string[] = [];
      vi.spyOn(hostedAccountsClient, "previewInvitation").mockImplementation(async () => {
        hashes.push(screen.getByTestId("location").textContent ?? "");
        return create(PreviewInvitationResponseSchema, { accountName: "Customer" });
      });
      render(
        <MemoryRouter initialEntries={[`/account-invitation#token=${encodeURIComponent(code)}`]}>
          <LocationReader />
          <AccountInvitation />
        </MemoryRouter>,
      );
      await screen.findByText("Join Customer");
      expect(hashes).toEqual([""]);
      expect(hostedAccountsClient.previewInvitation).toHaveBeenCalledWith(
        { token: code },
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      );
    },
  );

  it("previews a pasted code and joins with the existing login without putting the code in the URL", async () => {
    useFleetStore.setState({ auth: { ...initialAuth, isAuthenticated: true, username: "existing_user" } });
    const preview = vi
      .spyOn(hostedAccountsClient, "previewInvitation")
      .mockResolvedValue(create(PreviewInvitationResponseSchema, { accountName: "Customer" }));
    const accept = vi
      .spyOn(hostedAccountsClient, "acceptInvitation")
      .mockResolvedValue(create(AcceptInvitationResponseSchema, { accountId }));
    const user = userEvent.setup();
    openCodeEntry();
    expect(preview).not.toHaveBeenCalled();
    await user.click(screen.getByLabelText("Invitation code"));
    const pastedCode = "  k7mp - 4x9r - 6twc - 8nqf  ";
    await user.paste(pastedCode);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText("Join Customer");
    expect(preview).toHaveBeenCalledWith(
      { token: pastedCode },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(screen.getByTestId("location")).toBeEmptyDOMElement();
    await user.click(screen.getByRole("button", { name: "Accept invitation" }));
    await screen.findByText("Joined account");
    expect(accept).toHaveBeenCalledWith(
      { token: pastedCode },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });
  it.each([
    "K7MP4X9R6TWC8NQF",
    "k7mp4x9r6twc8nqf",
    token,
    "K7MP-4X9R6TWC8NQF",
    "K7MP4X9R-6TWC8NQF",
    "K7MP4X9R6TWC-8NQF",
    "K7MP-4X9R-6TWC8NQF",
    "K7MP-4X9R6TWC-8NQF",
    "K7MP4X9R-6TWC-8NQF",
    "  k7 mp - 4x 9r - 6t wc - 8n qf  ",
  ])("previews the supported form %j without replacing server normalization", async (code) => {
    const preview = vi
      .spyOn(hostedAccountsClient, "previewInvitation")
      .mockResolvedValue(create(PreviewInvitationResponseSchema, { accountName: "Customer" }));
    const user = userEvent.setup();
    openCodeEntry();
    await user.click(screen.getByLabelText("Invitation code"));
    await user.paste(code);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText("Join Customer");
    expect(preview).toHaveBeenCalledWith({ token: code }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  });

  it.each([
    "K7MP4X9R",
    "K7MP-4X9R",
    "K7MP4X9R6TWC8NQ",
    "K7MP4X9R6TWC8NQFF",
    "K7M-P4X9R6TWC8NQF",
    "K7MP-4X9-R6TWC8NQF",
    "K7MP4X9R6TW-C8NQF",
    "K7MP--4X9R-6TWC-8NQF",
    "K7MP_4X9R-6TWC-8NQF",
    "K7MP-4X0R-6TWC-8NQF",
    "K7MP-4X1R-6TWC-8NQF",
    "K7MP-4XIR-6TWC-8NQF",
    "K7MP-4XOR-6TWC-8NQF",
    "K7MP\t4X9R-6TWC-8NQF",
    "K7MP\u00a04X9R-6TWC-8NQF",
  ])("keeps malformed or ambiguous code %j out of preview", async (code) => {
    const preview = vi.spyOn(hostedAccountsClient, "previewInvitation");
    const user = userEvent.setup();
    openCodeEntry();
    await user.click(screen.getByLabelText("Invitation code"));
    await user.paste(code);
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
    expect(preview).not.toHaveBeenCalled();
  });

  it("does not preview incomplete codes and lets recipients replace unavailable invitations", async () => {
    const preview = vi
      .spyOn(hostedAccountsClient, "previewInvitation")
      .mockRejectedValueOnce(new ConnectError("Invitation expired", Code.PermissionDenied))
      .mockResolvedValueOnce(create(PreviewInvitationResponseSchema, { accountName: "New customer" }));
    const user = userEvent.setup();
    openCodeEntry();
    await user.type(screen.getByLabelText("Invitation code"), "incomplete");
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
    expect(preview).not.toHaveBeenCalled();
    await user.clear(screen.getByLabelText("Invitation code"));
    await user.type(screen.getByLabelText("Invitation code"), token);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText(/Invitation expired/);
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.getByLabelText("Invitation code")).toHaveValue("");
    expect(screen.queryByText(/Invitation expired/)).not.toBeInTheDocument();
    await user.type(screen.getByLabelText("Invitation code"), "T2CW-8V5N-K7MP-4X9R");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText("Join New customer");
  });
  it("cancels the old preview when changing codes and ignores its late result", async () => {
    let resolve!: (value: ReturnType<typeof create<typeof PreviewInvitationResponseSchema>>) => void;
    let signal!: AbortSignal;
    vi.spyOn(hostedAccountsClient, "previewInvitation").mockImplementationOnce((_request, options) => {
      signal = options!.signal!;
      return new Promise((r) => {
        resolve = r;
      });
    });
    const user = userEvent.setup();
    openCodeEntry();
    await user.type(screen.getByLabelText("Invitation code"), token);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(signal.aborted).toBe(true);
    await act(async () => resolve(create(PreviewInvitationResponseSchema, { accountName: "Old customer" })));
    expect(screen.queryByText("Join Old customer")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Invitation code")).toHaveValue("");
  });
});
