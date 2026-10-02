import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import MarketDataSection from "./MarketDataSection";
import {
  GetMarketDataResponseSchema,
  MarketMetricSchema,
} from "@/protoFleet/api/generated/marketdata/v1/marketdata_pb";
import { MARKET_DATA_POLL_MS } from "@/protoFleet/api/useMarketData";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const mocks = vi.hoisted(() => ({ get: vi.fn(), auth: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ marketDataClient: { getMarketData: mocks.get } }));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useAuthErrors: () => ({ handleAuthErrors: mocks.auth }),
}));

const settle = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

beforeEach(() => {
  vi.useFakeTimers();
  mocks.get.mockReset();
  mocks.auth.mockReset();
  useFleetStore.setState((state) => {
    state.auth.permissions = [];
  });
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("MarketDataSection rollout flag", () => {
  it("stays hidden and stops requesting data when the server denies permission", async () => {
    mocks.get.mockRejectedValue(new ConnectError("not permitted", Code.PermissionDenied));
    const { container } = render(<MarketDataSection />);
    await settle();
    expect(container).toBeEmptyDOMElement();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 3);
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(mocks.get).toHaveBeenCalledOnce();
  });

  it("accepts server-authorized site readers without an org-wide fleet permission", async () => {
    expect(useFleetStore.getState().auth.permissions).not.toContain("fleet:read");
    mocks.get.mockResolvedValue(create(GetMarketDataResponseSchema, { enabled: true }));
    render(<MarketDataSection />);
    await settle();
    expect(screen.getByRole("region", { name: "Market data" })).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
    });
    expect(mocks.get).toHaveBeenCalledTimes(2);
  });

  it("does not flash a panel while checking the flag", () => {
    mocks.get.mockImplementation(() => new Promise(() => {}));
    const { container } = render(<MarketDataSection />);
    expect(container).toBeEmptyDOMElement();
  });

  it("stays hidden with no periodic requests when the server disables it", async () => {
    mocks.get.mockResolvedValue(create(GetMarketDataResponseSchema));
    const { container } = render(<MarketDataSection />);
    await settle();
    expect(container).toBeEmptyDOMElement();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS * 3);
    });
    expect(mocks.get).toHaveBeenCalledOnce();
  });

  it("fails closed on an unanswered probe and shows the panel once enablement is confirmed", async () => {
    mocks.get.mockRejectedValueOnce(new Error("offline"));
    const { container } = render(<MarketDataSection />);
    await settle();
    expect(container).toBeEmptyDOMElement();
    mocks.get.mockResolvedValue(create(GetMarketDataResponseSchema, { enabled: true }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
    });
    expect(screen.getByRole("region", { name: "Market data" })).toBeInTheDocument();
    expect(screen.getAllByText("Unavailable")).toHaveLength(3);
  });

  it("hides an enabled panel when a later response disables it", async () => {
    mocks.get.mockResolvedValue(create(GetMarketDataResponseSchema, { enabled: true }));
    const { container } = render(<MarketDataSection />);
    await settle();
    expect(screen.getByRole("region", { name: "Market data" })).toBeInTheDocument();
    mocks.get.mockResolvedValue(create(GetMarketDataResponseSchema));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
    });
    expect(container).toBeEmptyDOMElement();
  });

  it.each([
    { hiddenMinutes: 3, refreshAlreadyPending: false },
    { hiddenMinutes: 61, refreshAlreadyPending: false },
    { hiddenMinutes: 61, refreshAlreadyPending: true },
  ])(
    "updates freshness immediately after $hiddenMinutes hidden minutes with a pending refresh ($refreshAlreadyPending)",
    async ({ hiddenMinutes, refreshAlreadyPending }) => {
      const retrievedAt = new Date("2026-10-02T12:00:00Z");
      vi.setSystemTime(retrievedAt);
      mocks.get.mockResolvedValue(
        create(GetMarketDataResponseSchema, {
          enabled: true,
          refreshIntervalSeconds: 60,
          bitcoinPriceUsd: create(MarketMetricSchema, {
            value: 100000,
            retrievedAt: timestampFromDate(retrievedAt),
          }),
        }),
      );
      const visibility = vi.spyOn(document, "visibilityState", "get");
      render(<MarketDataSection />);
      await settle();
      expect(screen.getByText("$100,000.00")).toBeInTheDocument();
      expect(screen.getByText("<1 min ago")).toBeInTheDocument();
      mocks.get.mockImplementationOnce(() => new Promise(() => {}));
      if (refreshAlreadyPending) {
        await act(async () => {
          await vi.advanceTimersByTimeAsync(MARKET_DATA_POLL_MS);
        });
      }
      visibility.mockReturnValue("hidden");
      act(() => document.dispatchEvent(new Event("visibilitychange")));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(hiddenMinutes * MARKET_DATA_POLL_MS);
      });
      visibility.mockReturnValue("visible");
      act(() => document.dispatchEvent(new Event("visibilitychange")));

      expect(mocks.get).toHaveBeenCalledTimes(2);
      if (hiddenMinutes >= 60) {
        expect(screen.queryByText("$100,000.00")).not.toBeInTheDocument();
        expect(screen.getAllByText("Unavailable")).toHaveLength(3);
      } else {
        expect(screen.getByText("$100,000.00")).toBeInTheDocument();
        expect(screen.getByText(/Stale ·/)).toBeInTheDocument();
        expect(screen.getByText("3 min ago")).toBeInTheDocument();
      }
    },
  );
});
