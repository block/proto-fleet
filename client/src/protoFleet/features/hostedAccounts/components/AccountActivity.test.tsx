import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountActivity from "./AccountActivity";
import { ActivityEntrySchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { AccountSchema, ListAccountActivitiesResponseSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

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
  render(
    <MemoryRouter>
      <AccountScopeContext.Provider
        value={{
          account: create(AccountSchema, { accountId, name: "Client A", depth: 1 }),
          client: client as AccountScope["client"],
          signal: controller.signal,
          refreshAccount: vi.fn(),
          invalidateAccount: vi.fn(),
        }}
      >
        <AccountActivity />
      </AccountScopeContext.Provider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
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
});
