import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { awaitEquipmentCommand } from "./awaitEquipmentCommand";

const read = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/api/clients", () => ({ containersClient: { getEquipmentCommandResult: read } }));
const result = { status: "finished", successCount: 1, failureCount: 0, deviceResults: [] };
beforeEach(() => {
  vi.useFakeTimers();
  read.mockReset();
});
afterEach(() => vi.useRealTimers());

it("does not read or allocate timers when already canceled", async () => {
  const abort = new AbortController();
  abort.abort();
  await expect(awaitEquipmentCommand("ct1", "batch", abort.signal)).rejects.toThrow("unconfirmed");
  expect(read).not.toHaveBeenCalled();
  expect(vi.getTimerCount()).toBe(0);
});

it("retries timed-out reads and waits for terminal completion", async () => {
  read.mockRejectedValueOnce(new ConnectError("waiting for lock", Code.DeadlineExceeded));
  read.mockResolvedValueOnce({ result: { ...result, status: "processing" } });
  read.mockResolvedValueOnce({ result });
  const pending = awaitEquipmentCommand("ct1", "batch");
  await vi.advanceTimersByTimeAsync(2000);
  expect(await pending).toBe(result);
  expect(read).toHaveBeenCalledTimes(3);
  expect(vi.getTimerCount()).toBe(0);
});

it.each([{ failureCount: 1 }, { successCount: 0 }, { detailsPruned: true }])(
  "rejects an unconfirmed terminal result: %j",
  async (fields) => {
    read.mockResolvedValue({ result: { ...result, ...fields } });
    await expect(awaitEquipmentCommand("ct1", "batch")).rejects.toThrow("state unconfirmed");
  },
);

it("bounds a hung result read even when the transport ignores cancellation", async () => {
  read.mockReturnValue(new Promise(() => undefined));
  const pending = expect(awaitEquipmentCommand("ct1", "batch")).rejects.toThrow("outcome unconfirmed");
  await vi.advanceTimersByTimeAsync(120_000);
  await pending;
  expect(read.mock.calls[0][1].signal.aborted).toBe(true);
  expect(vi.getTimerCount()).toBe(0);
});

it("cancels between polls without making another request", async () => {
  read.mockResolvedValue({ result: { ...result, status: "processing" } });
  const abort = new AbortController();
  const pending = expect(awaitEquipmentCommand("ct1", "batch", abort.signal)).rejects.toThrow("unconfirmed");
  await vi.advanceTimersByTimeAsync(0);
  abort.abort();
  await pending;
  expect(read).toHaveBeenCalledOnce();
  expect(vi.getTimerCount()).toBe(0);
});
