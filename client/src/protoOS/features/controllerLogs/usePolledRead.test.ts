import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CONTROLLER_RECORDS_POLL_MS, usePolledRead } from "./usePolledRead";

const { mockUseAuthRetry } = vi.hoisted(() => ({ mockUseAuthRetry: vi.fn() }));

vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({ useAuthRetry: mockUseAuthRetry }));

type Selection = { filter: string };
type Data = { rows: string[] };

const first: Selection = { filter: "a" };
const second: Selection = { filter: "b" };
const empty = (data: Data) => (data.rows.length ? undefined : "nothing sent");

const poll = () => act(() => vi.advanceTimersByTimeAsync(CONTROLLER_RECORDS_POLL_MS));

describe("usePolledRead", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    mockUseAuthRetry.mockReturnValue(
      ({
        request,
        onSuccess,
        onError,
      }: {
        request: (options: object) => Promise<unknown>;
        onSuccess: (data: unknown) => void;
        onError: (error: unknown) => void;
      }) => request({}).then(onSuccess, onError),
    );
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  const setup = (read: (selection: Selection) => Promise<Data>, inconclusive?: (data: Data) => string | undefined) =>
    renderHook(({ selection }) => usePolledRead(selection, read, inconclusive), { initialProps: { selection: first } });

  it("reports a failed first read as unavailable: an error and no data", async () => {
    const { result } = setup(() => Promise.reject(new Error("offline")));
    await act(() => vi.advanceTimersByTimeAsync(0));

    expect(result.current).toEqual({ data: undefined, error: "offline", loading: false });
  });

  it("keeps the data and adds the error when a refresh fails, then clears it on success", async () => {
    const read = vi
      .fn<(selection: Selection) => Promise<Data>>()
      .mockResolvedValueOnce({ rows: ["one"] })
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ rows: ["two"] });
    const { result } = setup(read);
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current.data).toEqual({ rows: ["one"] });

    await poll();
    expect(result.current).toEqual({ data: { rows: ["one"] }, error: "offline", loading: false });

    await poll();
    expect(result.current).toEqual({ data: { rows: ["two"] }, error: undefined, loading: false });
  });

  it("drops the previous selection's data when the selection changes", async () => {
    const read = vi.fn((selection: Selection) =>
      selection === first ? Promise.resolve({ rows: ["one"] }) : Promise.reject(new Error("offline")),
    );
    const { result, rerender } = setup(read);
    await act(() => vi.advanceTimersByTimeAsync(0));

    rerender({ selection: second });
    expect(result.current.loading).toBe(true);
    await act(() => vi.advanceTimersByTimeAsync(0));

    expect(result.current).toEqual({ data: undefined, error: "offline", loading: false });
  });

  it("shows an inconclusive first read but keeps conclusive data when a refresh is inconclusive", async () => {
    const read = vi
      .fn<(selection: Selection) => Promise<Data>>()
      .mockResolvedValueOnce({ rows: [] })
      .mockResolvedValueOnce({ rows: ["one"] })
      .mockResolvedValueOnce({ rows: [] });
    const { result } = setup(read, empty);
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current).toEqual({ data: { rows: [] }, error: undefined, loading: false });

    await poll();
    expect(result.current.data).toEqual({ rows: ["one"] });

    await poll();
    expect(result.current).toEqual({ data: { rows: ["one"] }, error: "nothing sent", loading: false });
  });
});
