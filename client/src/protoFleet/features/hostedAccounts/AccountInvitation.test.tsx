import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";
import AccountInvitation from "./AccountInvitation";
import { authClient, invitationClient } from "@/protoFleet/api/clients";
import { AuthenticateResponseSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import {
  AcceptInvitationResponseSchema,
  PreviewInvitationResponseSchema,
  RegisterInvitedUserResponseSchema,
} from "@/protoFleet/api/generated/invitation/v1/invitation_pb";
import { useFleetStore } from "@/protoFleet/store";
const initialAuth = useFleetStore.getState().auth;
const accountId = "11111111-1111-4111-8111-111111111111";
const token = "K7MP-4X9R";
afterEach(() => {
  vi.restoreAllMocks();
  useFleetStore.setState({ auth: initialAuth });
});
const LocationReader = () => <span data-testid="location">{useLocation().hash}</span>;
describe("invited login lifecycle", () => {
  it.each(["success", "failure"])("ignores an abandoned login's late %s after registration", async (outcome) => {
    vi.spyOn(invitationClient, "previewInvitation").mockResolvedValue(
      create(PreviewInvitationResponseSchema, { target: { case: "account", value: { accountName: "Customer" } } }),
    );
    vi.spyOn(invitationClient, "registerInvitedUser").mockResolvedValue(
      create(RegisterInvitedUserResponseSchema, { target: { case: "account", value: { accountId } } }),
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
    expect(invitationClient.registerInvitedUser).toHaveBeenCalledWith(
      { token, username: "new_user", password: "new-test-password" },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });
});

const codeBoxes = () =>
  Array.from({ length: 8 }, (_, index) =>
    screen.getByLabelText<HTMLInputElement>(`Invitation code character ${index + 1}`),
  );
const firstBox = () => codeBoxes()[0];
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
  it.each([token, " k7mp 4x9r "])("removes the invitation link fragment before previewing %j", async (code) => {
    const hashes: string[] = [];
    vi.spyOn(invitationClient, "previewInvitation").mockImplementation(async () => {
      hashes.push(screen.getByTestId("location").textContent ?? "");
      return create(PreviewInvitationResponseSchema, {
        target: { case: "account", value: { accountName: "Customer" } },
      });
    });
    render(
      <MemoryRouter initialEntries={[`/account-invitation#token=${encodeURIComponent(code)}`]}>
        <LocationReader />
        <AccountInvitation />
      </MemoryRouter>,
    );
    await screen.findByText("Join Customer");
    expect(hashes).toEqual([""]);
    expect(invitationClient.previewInvitation).toHaveBeenCalledWith(
      { token },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("previews a pasted code and joins with the existing login without putting the code in the URL", async () => {
    useFleetStore.setState({ auth: { ...initialAuth, isAuthenticated: true, username: "existing_user" } });
    const preview = vi
      .spyOn(invitationClient, "previewInvitation")
      .mockResolvedValue(
        create(PreviewInvitationResponseSchema, { target: { case: "account", value: { accountName: "Customer" } } }),
      );
    const accept = vi
      .spyOn(invitationClient, "acceptInvitation")
      .mockResolvedValue(create(AcceptInvitationResponseSchema, { target: { case: "account", value: { accountId } } }));
    const user = userEvent.setup();
    openCodeEntry();
    expect(preview).not.toHaveBeenCalled();
    expect(screen.getByText(/Enter your invitation code in the format/)).toHaveTextContent(
      "Enter your invitation code in the format XXXX-XXXX.",
    );
    expect(firstBox()).toHaveFocus();
    await user.paste("  k7mp - 4x9r  ");
    expect(
      codeBoxes()
        .map((box) => box.value)
        .join(""),
    ).toBe("K7MP4X9R");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText("Join Customer");
    expect(preview).toHaveBeenCalledWith({ token }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(screen.getByTestId("location")).toBeEmptyDOMElement();
    await user.click(screen.getByRole("button", { name: "Accept invitation" }));
    await screen.findByText("Joined account");
    expect(accept).toHaveBeenCalledWith({ token }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  });
  it.each(["K7MP4X9R", "k7mp4x9r", token, "K7M-P4X9R", "K7MP--4X9R", "K7MP4X9RR", "  k7 mp - 4x 9r  "])(
    "normalizes the pasted form %j into the canonical code",
    async (code) => {
      const preview = vi
        .spyOn(invitationClient, "previewInvitation")
        .mockResolvedValue(
          create(PreviewInvitationResponseSchema, { target: { case: "account", value: { accountName: "Customer" } } }),
        );
      const user = userEvent.setup();
      openCodeEntry();
      await user.click(firstBox());
      await user.paste(code);
      await user.click(screen.getByRole("button", { name: "Continue" }));
      await screen.findByText("Join Customer");
      expect(preview).toHaveBeenCalledWith({ token }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    },
  );

  it.each([
    "K7MP",
    "K7MP-4X9",
    "K7MP_4X9R",
    "K7MP-4X0R",
    "K7MP-4X1R",
    "K7MP-4XIR",
    "K7MP-4XOR",
    "K7MP\t4X9R",
    "K7MP\u00a04X9R",
  ])("keeps incomplete or ambiguous code %j out of preview", async (code) => {
    const preview = vi.spyOn(invitationClient, "previewInvitation");
    const user = userEvent.setup();
    openCodeEntry();
    await user.click(firstBox());
    await user.paste(code);
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
    expect(preview).not.toHaveBeenCalled();
  });

  it("explains characters that never appear in codes", async () => {
    const user = userEvent.setup();
    openCodeEntry();
    await user.paste("K7MP-4X0R");
    expect(screen.getByRole("alert")).toHaveTextContent("never contain 0, 1, I or O");
    expect(screen.getByLabelText("Invitation code character 7")).toHaveAttribute("aria-invalid", "true");
  });

  it("advances one box per character, skips typed hyphens and steps back on Backspace", async () => {
    const user = userEvent.setup();
    openCodeEntry();
    await user.keyboard("k7mp-4");
    expect(
      codeBoxes()
        .map((box) => box.value)
        .join(""),
    ).toBe("K7MP4");
    expect(screen.getByLabelText("Invitation code character 6")).toHaveFocus();
    await user.keyboard("{Backspace}");
    expect(screen.getByLabelText("Invitation code character 5")).toHaveFocus();
    expect(screen.getByLabelText("Invitation code character 5")).toHaveValue("");
    await user.keyboard("X9R");
    expect(
      codeBoxes()
        .map((box) => box.value)
        .join(""),
    ).toBe("K7MPX9R");
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
    await user.keyboard("{ArrowLeft}{ArrowLeft}");
    expect(screen.getByLabelText("Invitation code character 6")).toHaveFocus();
  });

  it("keeps both characters when two arrive in an empty box at once", () => {
    openCodeEntry();
    fireEvent.change(firstBox(), { target: { value: "k7" } });
    expect(
      codeBoxes()
        .map((box) => box.value)
        .join(""),
    ).toBe("K7");
    expect(screen.getByLabelText("Invitation code character 3")).toHaveFocus();
  });

  it("submits a complete code with Enter", async () => {
    const preview = vi
      .spyOn(invitationClient, "previewInvitation")
      .mockResolvedValue(
        create(PreviewInvitationResponseSchema, { target: { case: "account", value: { accountName: "Customer" } } }),
      );
    const user = userEvent.setup();
    openCodeEntry();
    await user.keyboard("K7MP4X9R{Enter}");
    await screen.findByText("Join Customer");
    expect(preview).toHaveBeenCalledWith({ token }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  });

  it("does not preview incomplete codes and lets recipients replace unavailable invitations", async () => {
    const preview = vi
      .spyOn(invitationClient, "previewInvitation")
      .mockRejectedValueOnce(new ConnectError("Invitation expired", Code.PermissionDenied))
      .mockResolvedValueOnce(
        create(PreviewInvitationResponseSchema, {
          target: { case: "account", value: { accountName: "New customer" } },
        }),
      );
    const user = userEvent.setup();
    openCodeEntry();
    await user.keyboard("K7MP");
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
    expect(preview).not.toHaveBeenCalled();
    await user.keyboard("4X9R");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText(/Invitation expired/);
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(codeBoxes().every((box) => box.value === "")).toBe(true);
    expect(screen.queryByText(/Invitation expired/)).not.toBeInTheDocument();
    await user.click(firstBox());
    await user.keyboard("T2CW-8V5N");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByText("Join New customer");
  });
  it("cancels the old preview when changing codes and ignores its late result", async () => {
    let resolve!: (value: ReturnType<typeof create<typeof PreviewInvitationResponseSchema>>) => void;
    let signal!: AbortSignal;
    vi.spyOn(invitationClient, "previewInvitation").mockImplementationOnce((_request, options) => {
      signal = options!.signal!;
      return new Promise((r) => {
        resolve = r;
      });
    });
    const user = userEvent.setup();
    openCodeEntry();
    await user.keyboard(token);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(signal.aborted).toBe(true);
    await act(async () =>
      resolve(
        create(PreviewInvitationResponseSchema, {
          target: { case: "account", value: { accountName: "Old customer" } },
        }),
      ),
    );
    expect(screen.queryByText("Join Old customer")).not.toBeInTheDocument();
    expect(codeBoxes().every((box) => box.value === "")).toBe(true);
  });
});

describe("team invitations", () => {
  const teamPreview = create(PreviewInvitationResponseSchema, {
    target: { case: "team", value: { organizationName: "Proto Mining", roleName: "FIELD_TECH" } },
  });
  const renderTeamInvitation = () =>
    render(
      <MemoryRouter initialEntries={[`/account-invitation#token=${token}`]}>
        <Routes>
          <Route path="/account-invitation" element={<AccountInvitation />} />
          <Route path="/" element={<div>Fleet home</div>} />
        </Routes>
      </MemoryRouter>,
    );

  it("previews the host team and its role", async () => {
    vi.spyOn(invitationClient, "previewInvitation").mockResolvedValue(teamPreview);
    renderTeamInvitation();
    expect(await screen.findByText("Join the fleet team")).toBeInTheDocument();
    expect(screen.getByText("You’re invited as Field Tech.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create login" })).toHaveAttribute("aria-current", "page");
  });

  it("explains unusable codes and returns to code entry", async () => {
    vi.spyOn(invitationClient, "previewInvitation").mockRejectedValue(
      new ConnectError("invitation unavailable", Code.FailedPrecondition),
    );
    const user = userEvent.setup();
    renderTeamInvitation();
    expect(await screen.findByText("This invitation can’t be used")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Enter another code" }));
    expect(codeBoxes().every((box) => box.value === "")).toBe(true);
  });

  it("refreshes the signed-in login's permissions after joining", async () => {
    useFleetStore.setState({
      auth: { ...initialAuth, isAuthenticated: true, username: "customer_user", role: "", permissions: [] },
    });
    vi.spyOn(invitationClient, "previewInvitation").mockResolvedValue(teamPreview);
    const accept = vi.spyOn(invitationClient, "acceptInvitation").mockResolvedValue(
      create(AcceptInvitationResponseSchema, {
        target: {
          case: "team",
          value: { userInfo: { username: "customer_user", role: "FIELD_TECH", permissions: ["miner:read"] } },
        },
      }),
    );
    const user = userEvent.setup();
    renderTeamInvitation();
    await user.click(await screen.findByRole("button", { name: "Accept invitation" }));
    await screen.findByText("Fleet home");
    expect(accept).toHaveBeenCalledWith({ token }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(useFleetStore.getState().auth.role).toBe("FIELD_TECH");
    expect(useFleetStore.getState().auth.permissions).toEqual(["miner:read"]);
  });

  it("signs out the current login so a different one can join", async () => {
    useFleetStore.setState({ auth: { ...initialAuth, isAuthenticated: true, username: "host_admin" } });
    vi.spyOn(invitationClient, "previewInvitation").mockResolvedValue(teamPreview);
    const accept = vi.spyOn(invitationClient, "acceptInvitation");
    const logout = vi
      .spyOn(authClient, "logout")
      .mockResolvedValue({} as Awaited<ReturnType<typeof authClient.logout>>);
    const user = userEvent.setup();
    renderTeamInvitation();
    expect(await screen.findByText("Join with your current login: host_admin")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Use a different login" }));

    expect(logout).toHaveBeenCalled();
    expect(useFleetStore.getState().auth.isAuthenticated).toBe(false);
    expect(await screen.findByRole("button", { name: "Create login" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByText("Join the fleet team")).toBeInTheDocument();
    expect(screen.queryByText("Fleet home")).not.toBeInTheDocument();
    expect(accept).not.toHaveBeenCalled();
  });

  it("signs a newly registered team member in and opens the fleet", async () => {
    vi.spyOn(invitationClient, "previewInvitation").mockResolvedValue(teamPreview);
    vi.spyOn(invitationClient, "registerInvitedUser").mockResolvedValue(
      create(RegisterInvitedUserResponseSchema, {
        target: { case: "team", value: { userInfo: { username: "new_tech", role: "FIELD_TECH" } } },
      }),
    );
    vi.spyOn(authClient, "authenticate").mockResolvedValue(
      create(AuthenticateResponseSchema, {
        sessionExpiry: 4000000000n,
        userInfo: { username: "new_tech", role: "FIELD_TECH", permissions: ["miner:read"] },
      }),
    );
    const user = userEvent.setup();
    renderTeamInvitation();
    await user.type(await screen.findByLabelText("Username"), "new_tech");
    await user.type(screen.getByLabelText("Choose a password"), "new-test-password");
    await user.click(screen.getByRole("button", { name: "Create login and join" }));
    await screen.findByText("Fleet home");
    expect(useFleetStore.getState().auth.permissions).toEqual(["miner:read"]);
  });
});
