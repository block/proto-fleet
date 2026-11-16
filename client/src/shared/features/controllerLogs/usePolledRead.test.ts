import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { usePolledRead } from "./usePolledRead";

type Selection = { filter: string };
type Data = { rows: string[] };

const POLL_MS = 10_000;
const first: Selection = { filter: "a" };
const second: Selection = { filter: "b" };
const empty = (data: Data) => (data.rows.length ? undefined : "nothing sent");

const poll = () => act(() => vi.advanceTimersByTimeAsync(POLL_MS));
const settle = () => act(() => vi.advanceTimersByTimeAsync(0));
const state = ({ data, error, loading }: { data?: Data; error?: string; loading: boolean }) => ({
  data,
  error,
  loading,
});

describe("usePolledRead", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  const setup = (
    read: (selection: Selection) => Promise<Data>,
    inconclusive?: (data: Data) => string | undefined,
    { polling = true } = {},
  ) =>
    renderHook(
      ({ selection }) =>
        usePolledRead(selection, read, { inconclusive, pollIntervalMs: polling ? POLL_MS : undefined }),
      {
        initialProps: { selection: first },
      },
    );

  it("reports a failed first read as unavailable: an error and no data", async () => {
    const { result } = setup(() => Promise.reject(new Error("offline")));
    await settle();

    expect(state(result.current)).toEqual({ data: undefined, error: "offline", loading: false });
  });

  it("keeps the data and adds the error when a refresh fails, then clears it on success", async () => {
    const read = vi
      .fn<(selection: Selection) => Promise<Data>>()
      .mockResolvedValueOnce({ rows: ["one"] })
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ rows: ["two"] });
    const { result } = setup(read);
    await settle();
    expect(result.current.data).toEqual({ rows: ["one"] });

    await poll();
    expect(state(result.current)).toEqual({ data: { rows: ["one"] }, error: "offline", loading: false });

    await poll();
    expect(state(result.current)).toEqual({ data: { rows: ["two"] }, error: undefined, loading: false });
  });

  it("drops the previous selection's data when the selection changes", async () => {
    const read = vi.fn((selection: Selection) =>
      selection === first ? Promise.resolve({ rows: ["one"] }) : Promise.reject(new Error("offline")),
    );
    const { result, rerender } = setup(read);
    await settle();

    rerender({ selection: second });
    expect(result.current.loading).toBe(true);
    await settle();

    expect(state(result.current)).toEqual({ data: undefined, error: "offline", loading: false });
  });

  it("shows an inconclusive first read but keeps conclusive data when a refresh is inconclusive", async () => {
    const read = vi
      .fn<(selection: Selection) => Promise<Data>>()
      .mockResolvedValueOnce({ rows: [] })
      .mockResolvedValueOnce({ rows: ["one"] })
      .mockResolvedValueOnce({ rows: [] });
    const { result } = setup(read, empty);
    await settle();
    expect(state(result.current)).toEqual({ data: { rows: [] }, error: undefined, loading: false });

    await poll();
    expect(result.current.data).toEqual({ rows: ["one"] });

    await poll();
    expect(state(result.current)).toEqual({ data: { rows: ["one"] }, error: "nothing sent", loading: false });
  });

  it("reads once per selection without an interval and re-reads on refresh", async () => {
    const read = vi
      .fn<(selection: Selection) => Promise<Data>>()
      .mockResolvedValueOnce({ rows: ["one"] })
      .mockResolvedValueOnce({ rows: ["two"] });
    const { result } = setup(read, undefined, { polling: false });
    await settle();
    await poll();
    expect(read).toHaveBeenCalledTimes(1);
    expect(result.current.data).toEqual({ rows: ["one"] });

    act(() => result.current.refresh());
    expect(result.current.data).toEqual({ rows: ["one"] });
    await settle();
    expect(read).toHaveBeenCalledTimes(2);
    expect(state(result.current)).toEqual({ data: { rows: ["two"] }, error: undefined, loading: false });
    expect(result.current.reading).toBe(false);
  });
});
