import { MemoryRouter, Route, Routes, useLocation, useNavigate, useParams } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import AccountRoutes from "./AccountRoutes";
import { HostedAccountsContext } from "./HostedAccountsContext";
import { AccountRole, AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";

vi.mock("./AccountDirectory", () => ({
  default: function HostDirectoryFixture() {
    const navigate = useNavigate();
    const location = useLocation();
    return (
      <div data-testid="host-list">
        <input aria-label="Host filter" defaultValue="" />
        <div>
          {location.search}
          {location.hash}
        </div>
        <button onClick={() => navigate("/accounts/client/overview")}>Preview client</button>
      </div>
    );
  },
}));
vi.mock("./AccountWorkspace", () => ({
  default: function ClientWorkspaceFixture({ onPreviewClose }: { onPreviewClose?: () => void }) {
    const navigate = useNavigate();
    const { section } = useParams();
    return (
      <div>
        <span>{section}</span>
        <button onClick={() => navigate("/accounts/client/assets")}>Client Fleet</button>
        <button onClick={onPreviewClose}>Close preview</button>
      </div>
    );
  },
}));
const initialAuth = useFleetStore.getState().auth;
afterEach(() => useFleetStore.setState({ auth: initialAuth }));

it("keeps the host list mounted and restores its URL, filter and scroll after client navigation", () => {
  useFleetStore.setState({ auth: { ...initialAuth, permissions: ["account:manage"] } });
  render(
    <MemoryRouter initialEntries={["/accounts?sort=descending#last-client"]}>
      <HostedAccountsContext.Provider
        value={{
          accounts: [create(AccountSchema, { accountId: "host", depth: 0, effectiveCapabilities: 16n })],
          hostAccess: true,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        <Routes>
          <Route path="/accounts/*" element={<AccountRoutes />} />
        </Routes>
      </HostedAccountsContext.Provider>
    </MemoryRouter>,
  );
  const list = screen.getByTestId("host-list");
  list.scrollTop = 240;
  fireEvent.change(screen.getByRole("textbox", { name: "Host filter" }), { target: { value: "Acme" } });
  fireEvent.click(screen.getByRole("button", { name: "Preview client" }));
  expect(list.closest("[inert]")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Client Fleet" }));
  expect(screen.getByText("assets")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Close preview" }));
  expect(screen.getByTestId("host-list")).toBe(list);
  expect(list.scrollTop).toBe(240);
  expect(screen.getByRole("textbox", { name: "Host filter" })).toHaveValue("Acme");
  expect(screen.getByText("?sort=descending#last-client")).toBeInTheDocument();
  expect(list.closest("[inert]")).not.toBeInTheDocument();
});

it("does not overlay a member workspace merely because the member can read Fleet", () => {
  useFleetStore.setState({ auth: { ...initialAuth, permissions: ["fleet:read"] } });
  render(
    <MemoryRouter initialEntries={["/accounts/client/overview"]}>
      <HostedAccountsContext.Provider
        value={{
          accounts: [
            create(AccountSchema, {
              accountId: "client",
              depth: 1,
              role: AccountRole.ADMINISTRATOR,
              effectiveCapabilities: 31n,
            }),
          ],
          hostAccess: true,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        <Routes>
          <Route path="/accounts/*" element={<AccountRoutes />} />
        </Routes>
      </HostedAccountsContext.Provider>
    </MemoryRouter>,
  );
  expect(screen.getByText("overview").closest("[inert]")).not.toBeInTheDocument();
  expect(screen.queryByTestId("host-list")).not.toBeInTheDocument();
});
