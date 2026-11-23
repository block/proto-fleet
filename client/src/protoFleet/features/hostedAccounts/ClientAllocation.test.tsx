import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import AccountRoutes from "./AccountRoutes";
import { HostedAccountsContext } from "./HostedAccountsContext";
import { AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { usePoolAssignmentProgress } from "@/protoFleet/features/poolConfigurations/assignmentProgress";

import { pushToast, STATUSES } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", () => ({ pushToast: vi.fn(), STATUSES: { success: "success" } }));

const rootId = "99999999-9999-4999-8999-999999999999";
const childId = "11111111-1111-4111-8111-111111111111";
const managementPath = `/accounts/${childId}/manage/assets`;
const legacyPath = `/accounts/${rootId}/children?assets=${childId}`;
const root = create(AccountSchema, {
  accountId: rootId,
  name: "Host",
  depth: 0,
  capabilityCeiling: 31n,
  effectiveCapabilities: 16n,
});
const Location = () => {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.pathname}
      {location.search}
    </output>
  );
};

beforeEach(() => {
  usePoolAssignmentProgress.setState({ jobs: [], detailsJobId: undefined, detailsSelection: undefined });
  vi.mocked(pushToast).mockClear();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
});

function mount(entry = managementPath, suspended = false) {
  const calls: { method: string; body: Record<string, unknown>; account: string | null }[] = [];
  const child = { accountId: childId, parentAccountId: rootId, name: "Customer", depth: 1, suspended };
  let allocated = false;
  let canManageAssignments = true;
  let conflict = false;
  let revision = "7";
  let release: (() => void) | undefined;
  let pending: Promise<void> | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
      const method = String(input).split("/").slice(-1)[0];
      const body = init?.body ? JSON.parse(new TextDecoder().decode(init.body as Uint8Array)) : {};
      calls.push({ method, body, account: new Headers(init?.headers).get("X-Fleet-Account-ID") });
      if (method === "AssignAccountAssets") {
        await pending;
        if (conflict) {
          conflict = false;
          revision = "8";
          return new Response(JSON.stringify({ code: "aborted", message: "Allocation changed" }), {
            status: 409,
            headers: { "Content-Type": "application/json" },
          });
        }
        allocated = true;
      }
      const assets = [
        { kind: "MINING_ASSET_KIND_MINER", id: "1", revision, name: "Miner One", available: true },
        { kind: "MINING_ASSET_KIND_MODULE", id: "1", revision: "9", name: "Module One", available: true },
        { kind: "MINING_ASSET_KIND_MINER", id: "2", revision: "3", name: "Unavailable", available: false },
        {
          kind: "MINING_ASSET_KIND_MINER",
          id: "3",
          revision: "4",
          name: "Outside policy",
          available: true,
          outsidePolicy: true,
        },
        { kind: "MINING_ASSET_KIND_MINER", id: "4", revision: "1", name: "Existing allocation", available: true },
      ].map((asset) => ({
        ...asset,
        ownerAccountId: asset.id === "4" || (allocated && asset.id === "1") ? childId : rootId,
      }));
      const reply =
        method === "ListChildren"
          ? { accounts: [child] }
          : method === "ListAccountAssets"
            ? {
                assets: assets.filter((asset) => asset.ownerAccountId === body.ownerAccountId),
                canManageAssignments,
                canViewAssignments: true,
                children: [child],
              }
            : {};
      return new Response(JSON.stringify(reply), { headers: { "Content-Type": "application/json" } });
    }),
  );
  render(
    <MemoryRouter initialEntries={[entry]}>
      <HostedAccountsContext.Provider
        value={{
          reportingAccess: false,
          accounts: [root],
          hostAccess: true,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        <Location />
        <Routes>
          <Route path="/accounts/*" element={<AccountRoutes />} />
        </Routes>
      </HostedAccountsContext.Provider>
    </MemoryRouter>,
  );
  return {
    calls,
    changeAccess: (allowed: boolean, restriction: "authority" | "suspension") => {
      if (restriction === "authority") canManageAssignments = allowed;
      else child.suspended = !allowed;
    },
    conflict: () => {
      conflict = true;
    },
    pause: () => {
      pending = new Promise<void>((resolve) => {
        release = resolve;
      });
    },
    resume: () => release?.(),
  };
}

async function openPicker(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: "Allocate assets" }));
  const picker = await screen.findByRole("dialog", { name: "Select assets" });
  await within(picker).findByText("Miner One");
  return within(picker);
}

describe("customer allocation picker", () => {
  it("explains private pool configurations in host administration", async () => {
    const { calls } = mount();
    const user = userEvent.setup();
    await screen.findByText("Existing allocation");
    expect(screen.getByRole("columnheader", { name: "Pool configuration" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Unavailable" }));
    const details = within(screen.getByRole("dialog", { name: "Pool configuration" }));
    await waitFor(() =>
      expect(details.getByText(/You don’t have access to view this hardware’s pool configuration/)).toBeVisible(),
    );
    expect(details.queryByText(/No saved pool configuration is assigned/)).not.toBeInTheDocument();
    expect(calls.some((call) => call.method === "GetAssignments")).toBe(false);
  });

  it("selects eligible assets and clears selection with the shared footer controls", async () => {
    const { calls } = mount();
    const user = userEvent.setup();
    const picker = await openPicker(user);
    await user.click(picker.getByRole("button", { name: "Select all" }));
    expect(picker.getByRole("checkbox", { name: "Select Miner One" })).toBeChecked();
    expect(picker.getByRole("checkbox", { name: "Select Module One" })).toBeChecked();
    expect(picker.getByRole("checkbox", { name: "Select Unavailable" })).not.toBeChecked();
    expect(picker.getByRole("checkbox", { name: "Select Outside policy" })).not.toBeChecked();
    expect(picker.getByText("2 assets selected")).toBeInTheDocument();
    await user.click(picker.getByRole("button", { name: "Select none" }));
    expect(picker.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    expect(picker.getByRole("checkbox", { name: "Select Module One" })).not.toBeChecked();
    expect(picker.getByText("0 assets selected")).toBeInTheDocument();
    expect(picker.getByRole("button", { name: "Allocate selected" })).toBeDisabled();
    expect(calls.some((call) => call.method === "AssignAccountAssets")).toBe(false);
  });

  it.each([managementPath, legacyPath])("selects miners and modules and refreshes in place at %s", async (entry) => {
    const { calls } = mount(entry);
    const user = userEvent.setup();
    const picker = await openPicker(user);
    expect(picker.queryByRole("button", { name: "Allocate to client" })).not.toBeInTheDocument();
    expect(picker.queryByRole("button", { name: "Manage columns" })).not.toBeInTheDocument();
    expect(picker.getByRole("checkbox", { name: "Select Unavailable" })).toBeDisabled();
    expect(picker.getByRole("checkbox", { name: "Select Outside policy" })).toBeDisabled();
    await user.click(picker.getByRole("checkbox", { name: "Select Miner One" }));
    await user.click(picker.getByRole("checkbox", { name: "Select Module One" }));
    await user.click(picker.getByRole("button", { name: "Allocate selected" }));
    const review = within(await screen.findByRole("dialog", { name: "Allocate 2 assets?" }));
    expect(review.getByText("Allocate the selected assets to Customer.")).toBeInTheDocument();
    expect(review.queryByText(/Host/)).not.toBeInTheDocument();
    expect(calls.some((call) => call.method === "AssignAccountAssets")).toBe(false);
    await user.click(review.getByRole("button", { name: "Allocate 2 assets" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByTestId("location")).toHaveTextContent(entry);
    expect(await screen.findByRole("button", { name: "Allocate assets" })).toHaveFocus();
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Allocated 2 assets to Customer.", status: STATUSES.success }),
    );
    expect(screen.queryByText("Allocated 2 assets to Customer.")).not.toBeInTheDocument();
    expect(screen.getByText("Miner One")).toBeInTheDocument();
    expect(screen.getByText("Module One")).toBeInTheDocument();
    expect(calls.filter((call) => call.method === "AssignAccountAssets")).toEqual([
      {
        method: "AssignAccountAssets",
        account: rootId,
        body: {
          accountId: rootId,
          childAccountId: childId,
          intent: "ASSET_ASSIGNMENT_INTENT_ASSIGN",
          assets: [
            { kind: "MINING_ASSET_KIND_MINER", id: "1", expectedRevision: "7" },
            { kind: "MINING_ASSET_KIND_MODULE", id: "1", expectedRevision: "9" },
          ],
        },
      },
    ]);
  });

  it("discards a canceled draft without changing location or allocations", async () => {
    const { calls } = mount();
    const user = userEvent.setup();
    await screen.findByText("Existing allocation");
    await user.click(screen.getByRole("button", { name: "Search mining assets" }));
    await user.type(screen.getByRole("textbox", { name: "Search mining assets" }), "Existing");
    await waitFor(() =>
      expect(calls).toContainEqual(
        expect.objectContaining({
          method: "ListAccountAssets",
          body: expect.objectContaining({ ownerAccountId: childId, search: "Existing" }),
        }),
      ),
    );
    const picker = await openPicker(user);
    await user.click(picker.getByRole("checkbox", { name: "Select Miner One" }));
    await user.click(picker.getByRole("button", { name: "Close dialog" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByTestId("location")).toHaveTextContent(managementPath);
    expect(screen.getByRole("textbox", { name: "Search mining assets" })).toHaveValue("Existing");
    const reopened = await openPicker(user);
    expect(reopened.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    expect(calls.some((call) => call.method === "AssignAccountAssets")).toBe(false);
  });

  it("requires fresh selection and review after an allocation conflict", async () => {
    const mock = mount();
    mock.conflict();
    const user = userEvent.setup();
    const picker = await openPicker(user);
    await user.click(picker.getByRole("checkbox", { name: "Select Miner One" }));
    await user.click(picker.getByRole("button", { name: "Allocate selected" }));
    await user.click(
      within(await screen.findByRole("dialog", { name: "Allocate 1 asset?" })).getByRole("button", {
        name: "Allocate 1 asset",
      }),
    );
    await picker.findByText("Allocations or equipment changed. Review the refreshed assets and select them again.");
    expect(picker.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    expect(screen.queryByRole("dialog", { name: "Allocate 1 asset?" })).not.toBeInTheDocument();
    expect(screen.getByTestId("location")).toHaveTextContent(managementPath);
    await user.click(picker.getByRole("checkbox", { name: "Select Miner One" }));
    await user.click(picker.getByRole("button", { name: "Allocate selected" }));
    await user.click(
      within(await screen.findByRole("dialog", { name: "Allocate 1 asset?" })).getByRole("button", {
        name: "Allocate 1 asset",
      }),
    );
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Allocated 1 asset to Customer.", status: STATUSES.success }),
    );
    expect(mock.calls.filter((call) => call.method === "AssignAccountAssets").slice(-1)[0]?.body.assets).toEqual([
      { kind: "MINING_ASSET_KIND_MINER", id: "1", expectedRevision: "8" },
    ]);
  });

  it("keeps the picker open while the confirmed allocation is pending", async () => {
    const mock = mount();
    mock.pause();
    const user = userEvent.setup();
    const picker = await openPicker(user);
    await user.click(picker.getByRole("checkbox", { name: "Select Miner One" }));
    await user.click(picker.getByRole("button", { name: "Allocate selected" }));
    await user.click(
      within(await screen.findByRole("dialog", { name: "Allocate 1 asset?" })).getByRole("button", {
        name: "Allocate 1 asset",
      }),
    );
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog", { name: "Select assets" })).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Allocate 1 asset?" })).toBeInTheDocument();
    expect(mock.calls.filter((call) => call.method === "AssignAccountAssets")).toHaveLength(1);
    await act(async () => mock.resume());
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Allocated 1 asset to Customer.", status: STATUSES.success }),
    );
  });

  it("disables allocation for a suspended customer", async () => {
    const { calls } = mount(managementPath, true);
    expect(await screen.findByRole("button", { name: "Allocate assets" })).toBeDisabled();
    expect(calls.some((call) => call.method === "AssignAccountAssets")).toBe(false);
  });

  it.each(["authority", "suspension"] as const)(
    "closes an open review on live %s loss and keeps the draft discarded",
    async (restriction) => {
      const polling = vi.spyOn(window, "setInterval");
      const mock = mount();
      const user = userEvent.setup();
      const picker = await openPicker(user);
      await user.click(picker.getByRole("checkbox", { name: "Select Miner One" }));
      await user.click(picker.getByRole("button", { name: "Allocate selected" }));
      await screen.findByRole("dialog", { name: "Allocate 1 asset?" });
      const poll = polling.mock.calls.find(([, interval]) => interval === POLL_INTERVAL_MS)?.[0];
      expect(poll).toBeTypeOf("function");
      const revalidate = async () =>
        act(async () => {
          if (typeof poll === "function") poll();
          window.dispatchEvent(new Event("focus"));
        });
      mock.changeAccess(false, restriction);
      await revalidate();
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      expect(mock.calls.some((call) => call.method === "AssignAccountAssets")).toBe(false);
      mock.changeAccess(true, restriction);
      await revalidate();
      await waitFor(() => expect(screen.getByRole("button", { name: "Allocate assets" })).toBeEnabled());
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    },
  );
});
