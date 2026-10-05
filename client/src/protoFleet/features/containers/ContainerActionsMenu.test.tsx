import { MemoryRouter, useLocation } from "react-router-dom";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import ContainerActionsMenu from "./ContainerActionsMenu";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

const state = vi.hoisted(() => ({ permissions: [] as string[] }));
vi.mock("@/protoFleet/store", () => ({ usePermissions: () => state.permissions }));

// JSDOM has no layout; keep the real menu and portal, bypassing only positioning.
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));

const container = create(ContainerSchema, {
  deviceIdentifier: "ct1/board",
  name: "Container 1",
});
const request = vi.fn<typeof fetch>();
const statusRequest = vi.fn<typeof fetch>();
const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status });
const openAction = async (name: string) => {
  fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
  const button = await screen.findByRole("button", { name });
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
};
const LocationProbe = () => <output data-testid="location">{useLocation().pathname}</output>;
const renderMenu = (overrides = {}) =>
  render(
    <MemoryRouter>
      <ContainerActionsMenu container={{ ...container, ...overrides }} />
      <LocationProbe />
    </MemoryRouter>,
  );

beforeEach(() => {
  state.permissions = [
    "miner:read",
    "miner:stop_mining",
    "miner:start_mining",
    "miner:set_power_target",
    "miner:set_cooling_mode",
    "miner:blink_led",
    "miner:firmware_update",
    "miner:reboot",
    "miner:update_pools",
    "pool:read",
    "miner:update_worker_names",
  ];
  request.mockReset().mockResolvedValue(json({}));
  statusRequest.mockReset().mockImplementation(async () => json({ mining: true }));
  vi.stubGlobal("fetch", (...args: Parameters<typeof fetch>) =>
    String(args[0]).endsWith("/mining/status") ? statusRequest(...args) : request(...args),
  );
});
afterEach(() => vi.unstubAllGlobals());

describe("container command dispatch", () => {
  it.each([
    ["Sleep container", "mining/stop"],
    ["Wake container", "mining/start"],
  ])("confirms %s and dispatches once to the controller", async (label, path) => {
    statusRequest.mockResolvedValueOnce(json({ mining: path === "mining/stop" }));
    renderMenu();
    await openAction(label);
    expect(request).not.toHaveBeenCalled();
    const dialog = screen.getByTestId("container-action-dialog");
    expect(dialog).toHaveTextContent("Container 1");
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenCalledTimes(1);
    expect(request).toHaveBeenCalledWith(
      `/api-proxy/miners/ct1%2Fboard/api/v2/${path}`,
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: "{}",
      }),
    );
  });

  it("cancels without issuing a command", async () => {
    renderMenu();
    await openAction("Sleep container");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(request).not.toHaveBeenCalled();
  });

  it.each([
    {
      source: "controller",
      body: { message: "Controller unavailable" },
      status: 503,
      detail: "Controller unavailable",
    },
    { source: "proxy authorization", body: { error: "Permission denied" }, status: 403, detail: "Permission denied" },
    {
      source: "proxy connection",
      body: { error: "Failed to connect to container" },
      status: 502,
      detail: "Failed to connect to container",
    },
  ])("shows $source errors and lets the operator retry", async ({ body, status, detail }) => {
    request.mockResolvedValueOnce(json(body, status)).mockResolvedValueOnce(json({}));
    renderMenu();
    await openAction("Sleep container");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(detail);
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("prevents duplicate dispatch while a command is pending", async () => {
    statusRequest.mockResolvedValueOnce(json({ mining: false }));
    let finish!: (response: Response) => void;
    request.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    renderMenu();
    await openAction("Wake container");
    const confirm = screen.getByRole("button", { name: "Confirm" });
    fireEvent.click(confirm);
    fireEvent.click(confirm);
    expect(confirm).toBeDisabled();
    expect(request).toHaveBeenCalledTimes(1);
    finish(json({}));
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
  });

  it("validates power bounds and preserves other target settings", async () => {
    request
      .mockResolvedValueOnce(
        json({
          watts: 1000,
          minimumWatts: 500,
          maximumWatts: 2000,
          performanceMode: "PERFORMANCE_MODE_NORMAL",
          balanceBays: true,
          hashOnDisconnect: true,
        }),
      )
      .mockResolvedValueOnce(json({ watts: 1500 }));
    renderMenu();
    await openAction("Manage power");
    const input = screen.getByLabelText("Container power target");
    await waitFor(() => expect(input).toHaveValue(1000));
    const confirm = screen.getByRole("button", { name: "Confirm" });
    for (const value of ["", "-1", "2001"]) {
      fireEvent.change(input, { target: { value } });
      expect(confirm).toBeDisabled();
    }
    fireEvent.change(input, { target: { value: "1500" } });
    fireEvent.click(confirm);
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenLastCalledWith(
      "/api-proxy/miners/ct1%2Fboard/api/v2/mining/target-power",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          watts: 1500,
          performanceMode: "PERFORMANCE_MODE_NORMAL",
          balanceBays: true,
          hashOnDisconnect: true,
        }),
      }),
    );
  });

  it.each([0, undefined])("requires at least 1 W when the controller minimum is %s", async (minimumWatts) => {
    request
      .mockResolvedValueOnce(json({ watts: 0, minimumWatts, maximumWatts: 2000 }))
      .mockResolvedValueOnce(json({ watts: 1 }));
    renderMenu();
    await openAction("Manage power");
    const input = screen.getByLabelText("Container power target");
    await waitFor(() => expect(input).toBeEnabled());
    const confirm = screen.getByRole("button", { name: "Confirm" });
    for (const value of ["0", "0.5", "-1"]) {
      fireEvent.change(input, { target: { value } });
      expect(confirm).toBeDisabled();
      fireEvent.click(confirm);
      expect(request).toHaveBeenCalledTimes(1); // Only the target read; no write.
    }
    expect(screen.getByText("Minimum 1 W; maximum 2,000 W.")).toBeInTheDocument();
    fireEvent.change(input, { target: { value: "1" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    expect(request).toHaveBeenLastCalledWith(
      "/api-proxy/miners/ct1%2Fboard/api/v2/mining/target-power",
      expect.objectContaining({ method: "PUT", body: JSON.stringify({ watts: 1 }) }),
    );
  });

  it.each([true, false])("offers only the applicable mining command when mining is %s", async (mining) => {
    statusRequest.mockResolvedValueOnce(json({ mining }));
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    const menu = await screen.findByTestId("container-ct1/board-actions-popover");
    await within(menu).findByRole("button", { name: mining ? "Sleep container" : "Wake container" });
    expect(
      within(menu)
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual([
      "View container",
      mining ? "Sleep container" : "Wake container",
      "Manage power",
      "Update firmware",
      "Edit pool",
      "Update worker name",
    ]);
    expect(statusRequest).toHaveBeenCalledWith(
      "/api-proxy/miners/ct1%2Fboard/api/v2/mining/status",
      expect.objectContaining({ method: "GET", credentials: "include" }),
    );
  });

  it("hides both commands while status is loading and refreshes after a command", async () => {
    let finish!: (response: Response) => void;
    statusRequest.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Wake container" })).not.toBeInTheDocument();
    finish(json({ mining: true }));
    fireEvent.click(await screen.findByRole("button", { name: "Sleep container" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument());
    statusRequest.mockResolvedValueOnce(json({ mining: false }));
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(await screen.findByRole("button", { name: "Wake container" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    expect(statusRequest).toHaveBeenCalledTimes(2);
  });

  it.each([
    ["MINING_OPERATING_STATE_STOPPED", true, "Wake container", "Sleep container"],
    ["MINING_OPERATING_STATE_NO_POOLS", false, "Sleep container", "Wake container"],
  ] as const)(
    "uses explicit %s status ahead of aggregate board mining",
    async (operatingState, mining, shown, hidden) => {
      statusRequest.mockResolvedValueOnce(json({ mining, operatingState }));
      renderMenu();
      fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
      expect(await screen.findByRole("button", { name: shown })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: hidden })).not.toBeInTheDocument();
    },
  );

  it("ignores an old status response after closing and reopening the menu", async () => {
    let finish!: (response: Response) => void;
    statusRequest.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Actions for Container 1" });
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(statusRequest.mock.calls[0][1]?.signal?.aborted).toBe(true);
    statusRequest.mockResolvedValueOnce(json({ mining: false }));
    fireEvent.click(trigger);
    expect(await screen.findByRole("button", { name: "Wake container" })).toBeInTheDocument();
    finish(json({ mining: true }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument());
  });

  it("keeps permission-limited menus available for a later status refresh", async () => {
    state.permissions = ["miner:read", "miner:start_mining"];
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Actions for Container 1" });
    fireEvent.click(trigger);
    expect(await screen.findByRole("button", { name: "No applicable commands" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    fireEvent.click(trigger);
    statusRequest.mockResolvedValueOnce(json({ mining: false }));
    fireEvent.click(trigger);
    expect(await screen.findByRole("button", { name: "Wake container" })).toBeInTheDocument();
  });

  it("keeps power available when the controller responds without a mining state", async () => {
    statusRequest.mockResolvedValueOnce(json({}));
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    expect(await screen.findByRole("button", { name: "Container status unavailable" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Wake container" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Manage power" })).toBeEnabled();
  });

  it("opens native container details using the controller identifier", async () => {
    renderMenu({ site: { id: 1n, label: "West site" } });
    await openAction("View container");
    expect(screen.getByTestId("location")).toHaveTextContent("/containers/ct1%2Fboard");
    expect(request).not.toHaveBeenCalled();
  });

  it("shows unsupported settings disabled without dispatching requests", async () => {
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    await screen.findByRole("button", { name: "Sleep container" });
    for (const name of ["Edit pool", "Update worker name", "Update firmware"]) {
      const button = screen.getByRole("button", { name });
      expect(button).toBeDisabled();
      fireEvent.click(button);
    }
    expect(screen.queryByTestId("container-action-dialog")).not.toBeInTheDocument();
    expect(request).not.toHaveBeenCalled();
  });

  it("offers readers only the container view", () => {
    state.permissions = ["miner:read"];
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    const menu = screen.getByTestId("container-ct1/board-actions-popover");
    expect(
      within(menu)
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual(["View container"]);
    expect(statusRequest).not.toHaveBeenCalled();
  });

  it.each([
    ["miner:reboot", "Update firmware"],
    ["miner:firmware_update", "Update firmware"],
    ["pool:read", "Edit pool"],
    ["miner:update_pools", "Edit pool"],
    ["miner:update_worker_names", "Update worker name"],
  ])("hides %s-dependent settings without that permission", async (permission, label) => {
    state.permissions = state.permissions.filter((key) => key !== permission);
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Container 1" }));
    await screen.findByRole("button", { name: "Sleep container" });
    expect(screen.queryByRole("button", { name: label })).not.toBeInTheDocument();
  });

  it("hides the menu without permission to read the container", () => {
    state.permissions = state.permissions.filter((key) => key !== "miner:read");
    renderMenu();
    expect(screen.queryByRole("button", { name: "Actions for Container 1" })).not.toBeInTheDocument();
  });

  it.each([
    [404, "miner not found or cannot be proxied"],
    [502, "Failed to connect to container"],
    [403, "Permission denied"],
  ])("keeps native navigation available when the proxy returns %s", async (code, error) => {
    statusRequest.mockResolvedValueOnce(json({ error }, code));
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Actions for Container 1" });
    expect(trigger).toBeEnabled();
    fireEvent.click(trigger);
    expect(await screen.findByRole("button", { name: "Container status unavailable" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Sleep container" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Wake container" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Manage power" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Manage power" }));
    expect(request).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "View container" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/containers/ct1%2Fboard");
  });

  it("checks proxy availability for operators with only power permissions", async () => {
    state.permissions = ["miner:read", "miner:set_power_target"];
    statusRequest.mockResolvedValueOnce(json({ error: "miner not found or cannot be proxied" }, 404));
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Actions for Container 1" });
    fireEvent.click(trigger);
    expect(screen.getByRole("button", { name: "Manage power" })).toBeDisabled();
    await waitFor(() => expect(statusRequest).toHaveBeenCalledTimes(1));
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    await waitFor(() => expect(screen.getByRole("button", { name: "Manage power" })).toBeEnabled());
    expect(statusRequest).toHaveBeenCalledTimes(2);
    expect(request).not.toHaveBeenCalled();
  });
});
