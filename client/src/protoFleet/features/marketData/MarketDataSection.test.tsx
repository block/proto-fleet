import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import MarketDataSection from "./MarketDataSection";
import { GetMarketDataResponseSchema } from "@/protoFleet/api/generated/marketdata/v1/marketdata_pb";
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
});
