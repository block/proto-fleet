import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create, toJsonString } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { type AccountScope, AccountScopeContext } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS, createAccountClient } from "../api";
import AccountActivity from "./AccountActivity";
import {
  ActivityEntrySchema,
  ListActivityFilterOptionsResponseSchema,
} from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { AccountSchema, ListAccountActivitiesResponseSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import * as utility from "@/shared/utils/utility";

const accountId = "11111111-1111-4111-8111-111111111111";
const entry = (eventId: string, description: string) =>
  create(ActivityEntrySchema, {
    eventId,
    description,
    eventType: "rename_hosted_account",
    eventCategory: "auth",
    scopeType: "account",
    result: "success",
    createdAt: { seconds: 1792000000n },
  });
const page = (authorityFingerprint: string, descriptions: string[], nextPageToken = "") =>
  create(ListAccountActivitiesResponseSchema, {
    authorityFingerprint,
    nextPageToken,
    totalCount: 2,
    activities: descriptions.map((description) => entry(description, description)),
  });

function mount(client: Partial<AccountScope["client"]>) {
  const controller = new AbortController();
  const invalidateAccount = vi.fn();
  render(
    <MemoryRouter>
      <AccountScopeContext.Provider
        value={{
          account: create(AccountSchema, { accountId, name: "Client A", depth: 1 }),
          client: client as AccountScope["client"],
          signal: controller.signal,
          refreshAccount: vi.fn(),
          invalidateAccount,
        }}
      >
        <AccountActivity />
      </AccountScopeContext.Provider>
    </MemoryRouter>,
  );
  return { invalidateAccount };
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("account activity", () => {
  it("discards accumulated pages when authority changes mid-pagination", async () => {
    let narrowed = false;
    const listAccountActivities = vi.fn(async (request: { pageToken?: string }) => {
      if (narrowed) return page("narrow", ["Fresh event"]);
      if (!request.pageToken) return page("broad", ["Broad first"], "next");
      narrowed = true;
      return page("narrow", ["Broad second"]);
    });
    mount({ listAccountActivities } as unknown as Partial<AccountScope["client"]>);
    expect(await screen.findByText("Broad first")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByText("Fresh event")).toBeInTheDocument();
    expect(screen.queryByText("Broad first")).not.toBeInTheDocument();
    expect(screen.queryByText("Broad second")).not.toBeInTheDocument();
    const calls = listAccountActivities.mock.calls;
    const restarted = calls[calls.length - 1]![0];
    expect(restarted.pageToken).toBeUndefined();
  });

  it("keeps filter chips mounted while a filtered page loads", async () => {
    const listAccountActivities = vi.fn(async (request: { filter?: { eventTypes: string[] } }) => {
      if (request.filter?.eventTypes.length) return new Promise<never>(() => {});
      const response = page("broad", ["Only event"]);
      response.filterOptions = create(ListActivityFilterOptionsResponseSchema, {
        eventTypes: [{ eventType: "rename_hosted_account", eventCategory: "auth" }],
      });
      return response;
    });
    mount({ listAccountActivities } as unknown as Partial<AccountScope["client"]>);
    await screen.findByText("Only event");
    fireEvent.click(screen.getByTestId("filter-nested-add-filter"));
    fireEvent.click(screen.getByTestId("nested-dropdown-filter-row-type"));
    fireEvent.click(await screen.findByTestId("filter-option-rename_hosted_account"));
    await waitFor(() => expect(listAccountActivities).toHaveBeenCalledTimes(2));
    expect(screen.queryByText("Only event")).not.toBeInTheDocument();
    expect(screen.getByTestId("filter-option-rename_hosted_account")).toBeInTheDocument();
  });

  it("retries a failed export from the export error banner", async () => {
    const listAccountActivities = vi.fn(async () => page("broad", ["Only event"]));
    let attempts = 0;
    const exportAccountActivities = vi.fn(async function* () {
      attempts++;
      yield* [] as { chunk: Uint8Array }[];
      throw new Error("export unavailable");
    });
    mount({ listAccountActivities, exportAccountActivities } as unknown as Partial<AccountScope["client"]>);
    await screen.findByText("Only event");
    fireEvent.click(screen.getByRole("button", { name: "Export activity CSV" }));
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
    await waitFor(() => expect(attempts).toBe(2));
    expect(listAccountActivities).toHaveBeenCalledTimes(1);
  });

  it("does not download a partial CSV when the stream fails after a chunk", async () => {
    const download = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
    const exportAccountActivities = vi.fn(async function* () {
      yield { chunk: new TextEncoder().encode("event,description\n") };
      throw new ConnectError("export interrupted", Code.Unavailable);
    });
    mount({
      listAccountActivities: vi.fn(async () => page("broad", ["Only event"])),
      exportAccountActivities,
    } as unknown as Partial<AccountScope["client"]>);
    await screen.findByText("Only event");
    fireEvent.click(screen.getByRole("button", { name: "Export activity CSV" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("export interrupted");
    expect(download).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Export activity CSV" })).toBeEnabled();
  });

  it("invalidates account authority when the CSV stream denies permission", async () => {
    const download = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
    const exportAccountActivities = vi.fn(async function* () {
      yield { chunk: new Uint8Array() };
      throw new ConnectError("activity access revoked", Code.PermissionDenied);
    });
    const { invalidateAccount } = mount({
      listAccountActivities: vi.fn(async () => page("broad", ["Only event"])),
      exportAccountActivities,
    } as unknown as Partial<AccountScope["client"]>);
    await screen.findByText("Only event");
    fireEvent.click(screen.getByRole("button", { name: "Export activity CSV" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("activity access revoked");
    expect(invalidateAccount).toHaveBeenCalledOnce();
    expect(download).not.toHaveBeenCalled();
  });

  it("cancels a pending export when refreshed activity has a different authority", async () => {
    const download = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
    let signal!: AbortSignal;
    let finish!: () => void;
    const pending = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const exportAccountActivities = vi.fn(async function* (_request: unknown, options: { signal: AbortSignal }) {
      signal = options.signal;
      yield { chunk: new TextEncoder().encode("broad authority data\n") };
      await pending;
      yield { chunk: new TextEncoder().encode("late broad authority data\n") };
    });
    mount({
      listAccountActivities: vi
        .fn()
        .mockResolvedValueOnce(page("broad", ["Broad event"]))
        .mockResolvedValue(page("narrow", ["Narrow event"])),
      exportAccountActivities,
    } as unknown as Partial<AccountScope["client"]>);
    await screen.findByText("Broad event");
    fireEvent.click(screen.getByRole("button", { name: "Export activity CSV" }));
    await waitFor(() => expect(signal).toBeDefined());
    expect(signal.aborted).toBe(false);
    fireEvent.focus(window);
    await screen.findByText("Narrow event");
    expect(signal.aborted).toBe(true);
    await act(async () => finish());
    expect(download).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Export activity CSV" })).toBeEnabled();
  });

  it.each(["first", "continuation"])("times out a stalled %s page and resumes polling", async (stalledPage) => {
    vi.useFakeTimers();
    const paginating = stalledPage === "continuation";
    const stalledRequest = paginating ? 3 : 2;
    let stalledSignal!: AbortSignal;
    let requests = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn((_input: Parameters<typeof fetch>[0], init: NonNullable<Parameters<typeof fetch>[1]>) => {
        requests++;
        if (requests === stalledRequest) {
          stalledSignal = init.signal!;
          return new Promise<Response>((_resolve, reject) => {
            stalledSignal.addEventListener("abort", () => reject(stalledSignal.reason), { once: true });
          });
        }
        return Promise.resolve(
          new Response(
            toJsonString(
              ListAccountActivitiesResponseSchema,
              page(
                "broad",
                [
                  requests <= stalledRequest || (paginating && requests === stalledRequest + 1)
                    ? "Before stall"
                    : "Recovered",
                ],
                paginating && requests <= stalledRequest + 1 ? "next" : "",
              ),
            ),
            { headers: { "Content-Type": "application/json" } },
          ),
        );
      }),
    );
    mount(createAccountClient(accountId, new AbortController().signal));
    await act(async () => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByText("Before stall")).toBeInTheDocument();
    if (paginating) {
      fireEvent.click(screen.getByRole("button", { name: "Load more" }));
      await act(async () => vi.advanceTimersByTimeAsync(0));
    } else await act(async () => vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS));
    expect(requests).toBe(stalledRequest);
    await act(async () => vi.advanceTimersByTimeAsync(ACCOUNT_READ_TIMEOUT_MS - 1));
    expect(stalledSignal.aborted).toBe(false);
    expect(requests).toBe(stalledRequest);
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(stalledSignal.aborted).toBe(true);
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByText("Before stall")).toBeInTheDocument();
    await act(async () => vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS));
    expect(screen.getByText("Recovered")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
