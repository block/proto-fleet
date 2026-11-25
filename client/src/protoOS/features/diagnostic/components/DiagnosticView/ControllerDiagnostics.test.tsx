import { act, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import ControllerDiagnostics from "./ControllerDiagnostics";
import type { ControllerDiagnosticsResult } from "@/protoOS/api/adapters/controllerDiagnostics";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";

const hosting = vi.hoisted(() => ({ adapter: null as MinerAdapter | null, isFleetHosted: false }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => hosting }));

const now = Date.now();

const withDiagnostics = (getControllerDiagnostics: () => Promise<ControllerDiagnosticsResult>) => {
  hosting.adapter = { getControllerDiagnostics } as unknown as MinerAdapter;
};

const card = (heading: string) => screen.getByRole("heading", { name: heading }).parentElement!.parentElement!;

describe("ControllerDiagnostics", () => {
  beforeEach(() => {
    hosting.adapter = null;
    hosting.isFleetHosted = false;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  test("renders nothing for adapters without controller diagnostics", () => {
    hosting.adapter = {} as MinerAdapter;
    const { container } = render(<ControllerDiagnostics />);
    expect(container).toBeEmptyDOMElement();
  });

  test("stays hidden and never polls when ProtoOS is embedded in Fleet", () => {
    const getControllerDiagnostics = vi.fn();
    hosting.adapter = { getControllerDiagnostics } as unknown as MinerAdapter;
    hosting.isFleetHosted = true;

    const { container } = render(<ControllerDiagnostics />);

    expect(container).toBeEmptyDOMElement();
    expect(getControllerDiagnostics).not.toHaveBeenCalled();
  });

  test("shows both controllers and keeps stale and missing observations distinct from healthy", async () => {
    withDiagnostics(async () => ({
      ha: {
        status: "reported",
        enabled: true,
        clusterId: "cluster-1",
        identityState: "confirmed",
        inventoryReady: true,
        observedAtMs: now,
        conflicts: [],
      },
      status: { status: "reported", databaseConnected: true },
      controllers: [
        {
          id: "ct1-a",
          local: true,
          reported: true,
          hostname: "controller-a",
          version: "4.2.0",
          role: "primary",
          writePrimary: true,
          roleObservation: { freshness: "fresh", observedAtMs: now },
          reachable: true,
          reachabilityObservation: { freshness: "fresh", observedAtMs: now },
          recovery: { state: "steady", phase: "steady" },
        },
        {
          id: "ct1-b",
          local: false,
          reported: true,
          role: "standby",
          writePrimary: false,
          roleObservation: { freshness: "stale", observedAtMs: now - 300_000 },
          reachable: false,
          recovery: { state: "reconciling", phase: "assignments", expectedBoards: 288, reconciledBoards: 120 },
        },
      ],
    }));

    render(<ControllerDiagnostics />);

    await screen.findByRole("heading", { name: "controller-a" });
    const local = card("controller-a");
    expect(within(local).getByText("This controller")).toBeInTheDocument();
    expect(within(local).getByText("4.2.0")).toBeInTheDocument();
    expect(local.querySelectorAll('[data-observation="fresh"]')).toHaveLength(3);
    expect(within(local).getByTestId("controller-role-status")).toHaveAttribute("data-status", "normal");
    expect(within(local).getByTestId("controller-reachability-status")).toHaveAttribute("data-status", "normal");

    const peer = card("ct1-b");
    expect(within(peer).getByText("Peer controller")).toBeInTheDocument();
    expect(within(peer).getByText("Unreachable")).toBeInTheDocument();
    // Role and write authority share one observation, so both read as stale.
    expect(within(peer).getAllByText("Stale · observed 5m ago")).toHaveLength(2);
    expect(within(peer).getByText("Not observed")).toBeInTheDocument();
    // A stale or missing observation never borrows the healthy colour of its last value.
    expect(within(peer).getByTestId("controller-role-status")).toHaveAttribute("data-status", "warning");
    expect(within(peer).getByTestId("controller-reachability-status")).toHaveAttribute("data-status", "inactive");
    expect(within(peer).getByText("Reconciling · Board assignments · 120 of 288 boards")).toBeInTheDocument();
    // Only the answering controller reports its own version.
    expect(within(peer).getAllByText("Not reported")).not.toHaveLength(0);
    expect(screen.getByText("Config store").nextSibling).toHaveTextContent("Not reported");
  });

  test("keeps the local controller and names the peer as unreported when HA status is unsupported", async () => {
    withDiagnostics(async () => ({
      ha: { status: "unsupported" },
      status: { status: "reported", databaseConnected: true },
      controllers: [
        { id: "ct1-a", local: true, reported: true, version: "4.2.0", role: "primary", writePrimary: true },
        { local: false, reported: false, role: "unknown" },
      ],
    }));

    render(<ControllerDiagnostics />);

    expect(
      await screen.findByText("This controller does not report HA status, so its peer cannot be shown."),
    ).toBeInTheDocument();
    expect(screen.getByText("High availability").nextSibling).toHaveTextContent("Not supported");
    expect(screen.getByRole("heading", { name: "ct1-a" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Peer controller" })).toBeInTheDocument();
    // Without an HA report the live role is shown without an observation to age.
    expect(document.querySelector("[data-observation]")).toBeNull();
  });

  test("qualifies an unreachable former primary's write authority as last reported", async () => {
    withDiagnostics(async () => ({
      ha: { status: "reported", enabled: true, identityState: "confirmed", inventoryReady: true, conflicts: [] },
      status: { status: "reported", databaseConnected: true },
      controllers: [
        {
          id: "ct1-b",
          local: true,
          reported: true,
          role: "primary",
          writePrimary: true,
          roleObservation: { freshness: "fresh", observedAtMs: now },
        },
        {
          id: "ct1-a",
          local: false,
          reported: true,
          role: "primary",
          writePrimary: true,
          roleObservation: { freshness: "stale", observedAtMs: now - 120_000 },
          reachable: false,
        },
        { id: "ct1-c", local: false, reported: true, role: "primary", writePrimary: true },
      ],
    }));

    render(<ControllerDiagnostics />);

    await screen.findByRole("heading", { name: "ct1-a" });
    const former = card("ct1-a");
    expect(within(former).getByText("Authorized")).toBeInTheDocument();
    expect(within(former).getByTestId("controller-writes-status")).toHaveAttribute("data-status", "warning");
    expect(within(former).getAllByText("Stale · observed 2m ago")).toHaveLength(2);

    const unobserved = card("ct1-c");
    expect(within(unobserved).getByTestId("controller-writes-status")).toHaveAttribute("data-status", "inactive");
    expect(within(unobserved).getAllByText("Not observed")).toHaveLength(3);

    expect(within(card("ct1-b")).getByTestId("controller-writes-status")).toHaveAttribute("data-status", "normal");
  });

  test("shows the live role without observations when HA is not enabled", async () => {
    withDiagnostics(async () => ({
      ha: { status: "reported", enabled: false, identityState: "unknown", inventoryReady: false, conflicts: [] },
      status: { status: "reported", databaseConnected: true },
      controllers: [
        { id: "ct1-a", local: true, reported: true, version: "4.2.0", role: "primary", writePrimary: true },
      ],
    }));

    render(<ControllerDiagnostics />);

    await screen.findByRole("heading", { name: "ct1-a" });
    expect(screen.getByText("High availability").nextSibling).toHaveTextContent("Not enabled");
    expect(screen.getByText("Primary")).toBeInTheDocument();
    expect(document.querySelector("[data-observation]")).toBeNull();
    expect(screen.queryByText("Not observed")).not.toBeInTheDocument();
  });

  test("keeps the last result on screen and keeps ageing it when a refresh fails", async () => {
    vi.useFakeTimers();
    const observedAtMs = Date.now();
    const getControllerDiagnostics = vi
      .fn()
      .mockResolvedValueOnce({
        ha: { status: "reported", enabled: true, identityState: "confirmed", inventoryReady: true, conflicts: [] },
        status: { status: "reported", databaseConnected: true },
        controllers: [
          {
            id: "ct1-a",
            local: true,
            reported: true,
            role: "primary",
            roleObservation: { freshness: "fresh", observedAtMs },
            reachable: true,
            reachabilityObservation: { freshness: "fresh", observedAtMs },
          },
        ],
      } satisfies ControllerDiagnosticsResult)
      .mockRejectedValue(Object.assign(new Error("offline"), { status: 503 }));
    hosting.adapter = { getControllerDiagnostics } as unknown as MinerAdapter;

    render(<ControllerDiagnostics />);
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByRole("heading", { name: "ct1-a" })).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();

    await act(() => vi.advanceTimersByTimeAsync(6_000));

    expect(getControllerDiagnostics).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("alert")).toHaveTextContent("Could not reach this controller.");
    expect(screen.getByRole("heading", { name: "ct1-a" })).toBeInTheDocument();
    expect(screen.getAllByText("Fresh · observed 6s ago")).toHaveLength(3);

    // Past the 15-second limit the retained observations read as stale.
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(screen.getAllByText("Stale · observed 16s ago")).toHaveLength(3);
  });

  test("reports a failed first load", async () => {
    withDiagnostics(() => Promise.reject(Object.assign(new Error("offline"), { status: 503 })));
    render(<ControllerDiagnostics />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not reach this controller.");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
