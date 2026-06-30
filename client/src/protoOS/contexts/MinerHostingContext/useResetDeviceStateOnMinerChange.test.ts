import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";

import { useResetDeviceStateOnMinerChange } from "./useResetDeviceStateOnMinerChange";
import useMinerStore from "@/protoOS/store/useMinerStore";

const seedPools = () => useMinerStore.getState().pools.setPoolsInfo([{ url: "stratum+tcp://a" }] as never);

describe("useResetDeviceStateOnMinerChange", () => {
  beforeEach(() => {
    useMinerStore.getState().pools.setPoolsInfo(undefined);
  });

  test("does not clear on first mount", () => {
    seedPools();
    renderHook(() => useResetDeviceStateOnMinerChange("/api-proxy/miners/a"));
    expect(useMinerStore.getState().pools.poolsInfo).toHaveLength(1);
  });

  test("clears device data when the miner key changes", () => {
    seedPools();
    const { rerender } = renderHook(({ k }) => useResetDeviceStateOnMinerChange(k), {
      initialProps: { k: "/api-proxy/miners/a" },
    });
    expect(useMinerStore.getState().pools.poolsInfo).toHaveLength(1);

    rerender({ k: "/api-proxy/miners/b" });
    expect(useMinerStore.getState().pools.poolsInfo).toBeUndefined();
  });

  test("does not clear when the key is unchanged", () => {
    const { rerender } = renderHook(({ k }) => useResetDeviceStateOnMinerChange(k), {
      initialProps: { k: "/api-proxy/miners/a" },
    });
    seedPools();
    rerender({ k: "/api-proxy/miners/a" });
    expect(useMinerStore.getState().pools.poolsInfo).toHaveLength(1);
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
