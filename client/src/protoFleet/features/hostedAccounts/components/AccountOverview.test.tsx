import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import { AccountScopeContext } from "../AccountScope";
import { createAccountConnection } from "../api";
import AccountOverview from "./AccountOverview";
import {
  AccountDashboardReadingSchema,
  AccountSchema,
  GetAccountDashboardResponseSchema,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

import type { LineChartProps } from "@/protoFleet/components/LineChart/LineChart";
import { useFleetStore } from "@/protoFleet/store";

const chart = vi.hoisted(() => vi.fn((_props: LineChartProps) => null));
vi.mock("@/protoFleet/components/LineChart", () => ({ default: chart }));

const initialUI = useFleetStore.getState().ui;
const accountId = "11111111-1111-4111-8111-111111111111";
const childId = "22222222-2222-4222-8222-222222222222";
const response = () =>
  create(GetAccountDashboardResponseSchema, {
    totals: {
      assetCount: 4n,
      hashboardCount: 4n,
      hashrateHs: 200e12,
      powerW: 6000,
      hashrateReportingCount: 4n,
      powerReportingCount: 3n,
      pairedReportingCount: 3n,
      hashingCount: 2n,
      offlineCount: 1n,
      unknownCount: 1n,
      temperatureC: 55,
      temperatureReportingCount: 2n,
      uptimePercent: 97,
      uptimeReportingCount: 3n,
    },
    sites: [{ siteId: 7n, name: "Client site", summary: { assetCount: 4n, hashboardCount: 4n } }],
    children: [{ accountId: childId, name: "Subclient A" }],
    historyAvailableFrom: { seconds: 1792000000n },
  });
beforeEach(() => {
  chart.mockClear();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});
afterEach(() => {
  useFleetStore.setState({ ui: initialUI });
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function mount(reply = response(), capabilities = 31n, readOnly = false) {
  const calls: { method: string; account: string | null; body: Record<string, unknown> }[] = [];
  const fetchMock = vi.fn(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    calls.push({
      method: String(input).split("/").slice(-1)[0]!,
      account: new Headers(init?.headers).get("X-Fleet-Account-ID"),
      body: JSON.parse(typeof init?.body === "string" ? init.body : new TextDecoder().decode(init?.body as Uint8Array)),
    });
    return new Response(JSON.stringify(toJson(GetAccountDashboardResponseSchema, reply)), {
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  const connection = createAccountConnection(accountId, { readOnly });
  const invalidate = vi.fn();
  const view = render(
    <MemoryRouter>
      <AccountScopeContext.Provider
        value={{
          account: create(AccountSchema, {
            accountId,
            name: "Client A",
            depth: 1,
            effectiveCapabilities: capabilities,
          }),
          client: connection.client,
          signal: connection.signal,
          readOnly,
          refreshAccount: vi.fn(),
          invalidateAccount: invalidate,
        }}
      >
        <AccountOverview />
      </AccountScopeContext.Provider>
    </MemoryRouter>,
  );
  return { ...view, calls, fetchMock, invalidate };
}

function chooseFilter(key: "allocation" | "site", id: string) {
  fireEvent.click(screen.getByRole("button", { name: "Add Filter" }));
  fireEvent.click(screen.getByTestId(`nested-dropdown-filter-row-${key}`));
  fireEvent.click(screen.getByTestId(`filter-option-${id}`));
  fireEvent.mouseDown(screen.getByRole("heading", { name: "Overview" }));
}

describe("shared account dashboard", () => {
  it("distinguishes hashrate-derived availability from the operational state chart", async () => {
    mount();
    expect(await screen.findByText("Hashing availability")).toBeInTheDocument();
    expect(screen.getByText("97.0%")).toBeInTheDocument();
    expect(screen.getByText(/Aggregate source-time freshness is unavailable/)).toBeInTheDocument();
  });
  it.each([
    ["7d", 3600],
    ["30d", 86400],
    ["90d", 86400],
    ["1y", 86400],
  ] as const)(
    "plots actual %s output buckets without false gaps or shifted coverage notes",
    async (duration, bucketSeconds) => {
      const reply = response();
      const lastBucket = Math.floor(Date.now() / 1000 / bucketSeconds) * bucketSeconds;
      const times = Array.from({ length: 5 }, (_, index) => lastBucket - (4 - index) * bucketSeconds);
      reply.readings = times.map((seconds, index) =>
        create(AccountDashboardReadingSchema, {
          time: { seconds: BigInt(seconds) },
          summary: {
            assetCount: 4n,
            moduleCount: 4n,
            ...(index === 2 || index === 4
              ? {}
              : {
                  hashrateHs: 400e12,
                  powerW: index === 3 ? 9000 : 10000,
                  efficiencyJTh: index === 3 ? undefined : 25,
                  hashrateReportingCount: 4n,
                  powerReportingCount: index === 3 ? 3n : 4n,
                  pairedReportingCount: index === 3 ? 3n : 4n,
                }),
          },
        }),
      );
      reply.readings[2].summary = undefined;
      const { calls } = mount(reply);
      await screen.findByText("200.0 TH/s");
      fireEvent.click(screen.getByRole("button", { name: duration }));
      await waitFor(() => expect(calls).toHaveLength(2));
      await waitFor(() =>
        expect(chart).toHaveBeenCalledWith(
          expect.objectContaining({ duration, chartData: expect.any(Array) }),
          undefined,
        ),
      );
      for (const [aggregateKey, values] of [
        ["hashrate", [400, 400, null, 400, null]],
        ["power", [10, 10, null, 9, null]],
        ["efficiency", [25, 25, null, null, null]],
      ] as const) {
        const props = [...chart.mock.calls].reverse().find(([props]) => props.aggregateKey === aggregateKey)![0];
        expect(props.chartData!.map((point) => point.datetime)).toEqual(times.map((time) => time * 1000));
        expect(props.chartData!.map((point) => point[aggregateKey])).toEqual(values);
        expect(props.connectNulls).toBe(false);
      }
      const power = [...chart.mock.calls].reverse().find(([props]) => props.aggregateKey === "power")![0];
      expect(power.tooltipNotes![times[3] * 1000]).toBe("Legacy module data · 3 of 4 modules reporting");
    },
  );
  it.each([false, true])("requests each Fleet range with shared granularity (preview=%s)", async (readOnly) => {
    const { calls } = mount(response(), 31n, readOnly);
    await screen.findByText("200.0 TH/s");
    const ranges = within(screen.getByRole("group", { name: "Time range" }));
    const durations = [
      ["1h", 1, 90],
      ["24h", 24, 90],
      ["7d", 7 * 24, 900],
      ["30d", 30 * 24, 2700],
      ["90d", 90 * 24, 8100],
      ["1y", 365 * 24, 32850],
    ] as const;
    expect(ranges.getAllByRole("button").map((button) => button.textContent)).toEqual(
      durations.map(([label]) => label),
    );
    for (const [index, [duration, hours, bucketSeconds]] of durations.entries()) {
      if (index > 0) fireEvent.click(ranges.getByRole("button", { name: duration }));
      await waitFor(() => expect(calls).toHaveLength(index + 1));
      expect(calls[index]).toMatchObject({
        method: "GetAccountDashboard",
        account: accountId,
        body: { accountId, bucketSeconds },
      });
      expect(Date.parse(String(calls[index].body.endTime)) - Date.parse(String(calls[index].body.startTime))).toBe(
        hours * 60 * 60 * 1000,
      );
      expect(ranges.getByRole("button", { name: duration })).toHaveAttribute("aria-pressed", "true");
    }
  });
  it("surfaces uptime gaps against the latest historical allocation rather than current inventory", async () => {
    const reply = response();
    reply.readings = [
      create(AccountDashboardReadingSchema, {
        time: { seconds: BigInt(Math.floor(Date.now() / 1000)) },
        summary: { assetCount: 3n },
        uptimeStatus: { timestamp: { seconds: BigInt(Math.floor(Date.now() / 1000)) }, hashingCount: 2 },
      }),
    ];
    mount(reply);
    await screen.findByText("2 of 3 assets reporting");
    expect(screen.queryByText("All assets hashing")).not.toBeInTheDocument();
  });
  it("shows status-only history without claiming there are no historical readings", async () => {
    const reply = response();
    reply.readings = [
      create(AccountDashboardReadingSchema, {
        time: { seconds: BigInt(Math.floor(Date.now() / 1000)) },
        summary: { assetCount: 4n },
        uptimeStatus: { timestamp: { seconds: BigInt(Math.floor(Date.now() / 1000)) }, notHashingCount: 4 },
      }),
    ];
    mount(reply);
    await screen.findByText("100% not hashing");
    expect(screen.queryByText(/No authorized historical readings/)).not.toBeInTheDocument();
  });
  it("labels legacy module readings and keeps their power in kilowatts", async () => {
    const reply = response();
    const summary = { assetCount: 1n, moduleCount: 1n, powerW: 200_000, powerReportingCount: 1n };
    reply.readings = [
      create(AccountDashboardReadingSchema, {
        time: { seconds: BigInt(Math.floor(Date.now() / 1000)) },
        summary,
      }),
    ];
    mount(reply);
    await screen.findByRole("region", { name: "Current readings" });
    const performance = within(screen.getByRole("region", { name: "Performance" }));
    const power = performance
      .getAllByTestId("stats-item")
      .find((item) => within(item).queryByRole("heading", { name: "Power" }))!;
    expect(power).toHaveTextContent("200.0");
    expect(power).toHaveTextContent("kW");
    expect(performance.getByText(/This range includes legacy module data/)).toBeInTheDocument();
  });
  it("updates current and historical temperatures when personal units change", async () => {
    const reply = response();
    reply.readings = [
      create(AccountDashboardReadingSchema, {
        time: { seconds: BigInt(Math.floor(Date.now() / 1000)) },
        summary: reply.totals,
        temperatureStatus: {
          timestamp: { seconds: BigInt(Math.floor(Date.now() / 1000)) },
          okCount: 1,
          hotCount: 1,
        },
        uptimeStatus: {
          timestamp: { seconds: BigInt(Math.floor(Date.now() / 1000)) },
          hashingCount: 1,
          brokenCount: 1,
        },
      }),
    ];
    useFleetStore.getState().ui.setTemperatureUnit("F");
    mount(reply);
    const current = within(await screen.findByRole("region", { name: "Current readings" }));
    expect(await current.findByText("131.0 °F")).toBeInTheDocument();
    const temperature = within(screen.getByRole("region", { name: "Performance" }))
      .getAllByTestId("stats-item")
      .find((item) => within(item).queryByRole("heading", { name: "Temperature" }))!;
    expect(temperature).toHaveTextContent("1 asset outside of safe range");
    expect(screen.getAllByText("50% of reporting assets").length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: /miner$/ })).not.toBeInTheDocument();
    act(() => useFleetStore.getState().ui.setTemperatureUnit("C"));
    expect(current.getByText("55.0 °C")).toBeInTheDocument();
    expect(temperature).toHaveTextContent("1 asset outside of safe range");
    expect(screen.queryByText(/This range includes legacy module data/)).not.toBeInTheDocument();
  });
  it("uses scoped Dashboard health and all five performance charts with honest current coverage", async () => {
    const { calls } = mount();
    await screen.findByText("200.0 TH/s");
    expect(screen.getByText("4 assets")).toBeInTheDocument();
    expect(screen.getAllByText("3 of 4 hashboards reporting")).toHaveLength(2);
    expect(screen.queryByText("30 J/TH")).not.toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Performance" })).toBeInTheDocument();
    for (const label of ["Hashrate", "Power", "Efficiency", "Temperature", "Hashing availability"])
      expect(screen.getAllByRole("heading", { name: label }).length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "Add Filter" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Refresh" })).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(calls[0].method).toBe("GetAccountDashboard");
    expect(calls[0].account).toBe(accountId);
    expect(calls[0].body.accountId).toBe(accountId);
    expect(Date.parse(String(calls[0].body.endTime)) - Date.parse(String(calls[0].body.startTime))).toBe(
      60 * 60 * 1000,
    );
    expect(
      within(screen.getByRole("group", { name: "Time range" })).getByRole("button", { name: "1h" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("link", { name: "View mining assets" })).toHaveAttribute(
      "href",
      `/accounts/${accountId}/assets`,
    );
  });
  it("sends allocation and site filters to the server while retaining parent navigation", async () => {
    const { calls } = mount();
    await screen.findByText("200.0 TH/s");
    chooseFilter("allocation", childId);
    await waitFor(() => expect(calls.slice(-1)[0]?.body.ownerAccountId).toBe(childId));
    chooseFilter("site", "7");
    await waitFor(() => expect(calls.slice(-1)[0]?.body.siteId).toBe("7"));
    expect(calls.slice(-1)[0]?.body.accountId).toBe(accountId);
    expect(screen.queryByRole("link", { name: "Subclient A" })).not.toBeInTheDocument();
  });
  it("keeps selected authorized filter options visible while the next request is pending", async () => {
    const { fetchMock } = mount();
    await screen.findByText("200.0 TH/s");
    fetchMock.mockImplementation(() => new Promise<Response>(() => {}));
    chooseFilter("allocation", childId);
    expect(screen.getByTestId("active-filter-allocation")).toHaveTextContent("Subclient A");
    chooseFilter("site", "7");
    expect(screen.getByTestId("active-filter-site")).toHaveTextContent("Client site");
    expect(screen.queryByText("200.0 TH/s")).not.toBeInTheDocument();
  });
  it("keeps duration controls mounted and focused while duration and filters reload", async () => {
    const { fetchMock } = mount();
    await screen.findByText("200.0 TH/s");
    const ranges = screen.getByRole("group", { name: "Time range" });
    const day = within(ranges).getByRole("button", { name: "24h" });
    fetchMock.mockImplementation(() => new Promise<Response>(() => {}));
    day.focus();
    fireEvent.click(day);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(screen.getByRole("group", { name: "Time range" })).toBe(ranges);
    expect(day).toHaveFocus();
    expect(day).toHaveAttribute("aria-pressed", "true");
    expect(screen.queryByText("200.0 TH/s")).not.toBeInTheDocument();
    chooseFilter("site", "7");
    expect(screen.getByRole("group", { name: "Time range" })).toBe(ranges);
  });
  it.each([
    ["invalid_argument", 400],
    ["unavailable", 503],
    ["deadline_exceeded", 504],
    ["internal", 500],
    ["unknown", 500],
    ["resource_exhausted", 429],
  ])("lets a %s history request recover by selecting one hour", async (code, status) => {
    const { fetchMock } = mount();
    await screen.findByText("200.0 TH/s");
    const ranges = screen.getByRole("group", { name: "Time range" });
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ code, message: "Choose a shorter time range or filter assets" }), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    );
    fireEvent.click(within(ranges).getByRole("button", { name: "30d" }));
    await screen.findByText(/Choose a shorter time range/);
    expect(screen.getByRole("group", { name: "Time range" })).toBe(ranges);
    expect(screen.queryByText("200.0 TH/s")).not.toBeInTheDocument();
    fireEvent.click(within(ranges).getByRole("button", { name: "1h" }));
    expect(await screen.findByText("200.0 TH/s")).toBeInTheDocument();
    expect(screen.queryByText(/Choose a shorter time range/)).not.toBeInTheDocument();
    expect(within(ranges).getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "true");
  });
  it("cannot restore filter names from a late response after access is denied", async () => {
    const { fetchMock } = mount();
    await screen.findByText("200.0 TH/s");
    let finish!: (response: Response) => void;
    fetchMock.mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    );
    chooseFilter("allocation", childId);
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ code: "permission_denied", message: "Access revoked" }), {
        status: 403,
        headers: { "Content-Type": "application/json" },
      }),
    );
    chooseFilter("site", "7");
    await screen.findByText(/Access revoked/);
    await act(async () =>
      finish(
        new Response(JSON.stringify(toJson(GetAccountDashboardResponseSchema, response())), {
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    expect(screen.queryByText("Client site")).not.toBeInTheDocument();
    expect(screen.queryByText("Subclient A")).not.toBeInTheDocument();
    expect(screen.queryByText("200.0 TH/s")).not.toBeInTheDocument();
  });
  it("replaces an allocation selection and clears both filters back to the whole estate", async () => {
    const { calls } = mount();
    await screen.findByText("200.0 TH/s");
    chooseFilter("allocation", childId);
    await waitFor(() => expect(calls.slice(-1)[0]?.body.ownerAccountId).toBe(childId));
    fireEvent.click(screen.getByTestId("active-filter-allocation-edit"));
    fireEvent.click(screen.getByTestId(`filter-option-${accountId}`));
    fireEvent.mouseDown(screen.getByRole("heading", { name: "Overview" }));
    await waitFor(() => expect(calls.slice(-1)[0]?.body.ownerAccountId).toBe(accountId));
    expect(screen.getByTestId("active-filter-allocation")).toHaveTextContent("Directly managed");
    expect(screen.getByTestId("active-filter-allocation")).not.toHaveTextContent("Subclient A");
    chooseFilter("site", "7");
    await waitFor(() => expect(calls.slice(-1)[0]?.body.siteId).toBe("7"));
    fireEvent.click(screen.getByRole("button", { name: "Clear Allocation filter" }));
    fireEvent.click(screen.getByRole("button", { name: "Clear Site filter" }));
    await waitFor(() => {
      expect(calls.slice(-1)[0]?.body.ownerAccountId).toBeUndefined();
      expect(calls.slice(-1)[0]?.body.siteId).toBeUndefined();
    });
  });
  it("does not offer private allocation filters to viewers", async () => {
    mount(response(), 1n);
    await screen.findByText("200.0 TH/s");
    fireEvent.click(screen.getByRole("button", { name: "Add Filter" }));
    expect(screen.queryByTestId("nested-dropdown-filter-row-allocation")).not.toBeInTheDocument();
    expect(screen.queryByText("Subclient A")).not.toBeInTheDocument();
  });
  it("shows an empty state when historical readings are unavailable", async () => {
    mount();
    expect(await screen.findByText(/No authorized historical readings/)).toBeInTheDocument();
  });
  it("preserves permitted chart history after recall while exposing no current asset detail", async () => {
    const reply = response();
    reply.totals = create(GetAccountDashboardResponseSchema, { totals: {} }).totals;
    reply.readings = [
      {
        $typeName: "hosted.v1.AccountDashboardReading",
        time: { $typeName: "google.protobuf.Timestamp", seconds: BigInt(Math.floor(Date.now() / 1000)), nanos: 0 },
        summary: response().totals,
      },
    ];
    mount(reply);
    expect(await screen.findByText("No mining assets")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Performance" })).toBeInTheDocument();
    expect(screen.queryByText(/No authorized historical readings/)).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /module detail/i })).not.toBeInTheDocument();
  });
  it("clears metrics on denied refresh and invalidates live account authority", async () => {
    const { fetchMock, invalidate } = mount();
    await screen.findByText("200.0 TH/s");
    fetchMock.mockImplementation(
      async () =>
        new Response(JSON.stringify({ code: "permission_denied", message: "Access revoked" }), {
          status: 403,
          headers: { "Content-Type": "application/json" },
        }),
    );
    act(() => window.dispatchEvent(new Event("focus")));
    await screen.findByText(/Access revoked/);
    expect(screen.queryByText("200.0 TH/s")).not.toBeInTheDocument();
    expect(screen.queryByText("Client site")).not.toBeInTheDocument();
    expect(screen.queryByText("Subclient A")).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "Time range" })).not.toBeInTheDocument();
    await waitFor(() => expect(invalidate).toHaveBeenCalled());
  });
});
