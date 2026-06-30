import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";

import { useResetDeviceStateOnMinerChange } from "./useResetDeviceStateOnMinerChange";
import useMinerStore from "@/protoOS/store/useMinerStore";

const seedPools = () => useMinerStore.getState().pools.setPoolsInfo([{ url: "stratum+tcp://a" }] as never);
const seedHardware = () => useMinerStore.getState().hardware.setHashboards([{ serial: "HB-A-1" }] as never);
const poolsCount = () => useMinerStore.getState().pools.poolsInfo?.length ?? 0;
const hashboardCount = () => useMinerStore.getState().hardware.hashboards.size;

describe("useResetDeviceStateOnMinerChange", () => {
  beforeEach(() => {
    useMinerStore.getState().pools.setPoolsInfo(undefined);
    useMinerStore.getState().hardware.reset();
  });

  test("clears stale device data on first fleet mount (close-then-reopen)", () => {
    // Residual data from a previously-viewed miner, store survived the unmount.
    seedPools();
    seedHardware();

    renderHook(() => useResetDeviceStateOnMinerChange("/api-proxy/miners/b"));

    expect(poolsCount()).toBe(0);
    expect(hashboardCount()).toBe(0);
  });

  test("clears device data when the miner key changes in place", () => {
    const { rerender } = renderHook(({ k }) => useResetDeviceStateOnMinerChange(k), {
      initialProps: { k: "/api-proxy/miners/a" },
    });
    // Populate after mount, then switch miners.
    seedPools();
    seedHardware();
    expect(poolsCount()).toBe(1);

    rerender({ k: "/api-proxy/miners/b" });
    expect(poolsCount()).toBe(0);
    expect(hashboardCount()).toBe(0);
  });

  test("does nothing in direct mode (empty key)", () => {
    seedPools();
    renderHook(() => useResetDeviceStateOnMinerChange(""));
    expect(poolsCount()).toBe(1);
  });

  test("preserves UI preferences across a miner change", () => {
    const theme = useMinerStore.getState().ui.theme;
    const { rerender } = renderHook(({ k }) => useResetDeviceStateOnMinerChange(k), {
      initialProps: { k: "/api-proxy/miners/a" },
    });
    rerender({ k: "/api-proxy/miners/b" });
    expect(useMinerStore.getState().ui.theme).toBe(theme);
  });
});
