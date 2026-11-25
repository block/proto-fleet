import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { hostedAccountsClient } from "../api";
import ReportingSetup from "./ReportingSetup";
import { authClient } from "@/protoFleet/api/clients";
import { ListUsersResponseSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import {
  HostReportingSetupSchema,
  ListHostReportingFactsResponseSchema,
  MutateHostReportingInputsResponseSchema,
  ReportingFactKind,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { SitesContext, type SitesContextValue } from "@/protoFleet/api/SitesContext";
import { useFleetStore } from "@/protoFleet/store";

beforeEach(() => {
  vi.spyOn(hostedAccountsClient, "listHostReportingFacts").mockResolvedValue(
    create(ListHostReportingFactsResponseSchema),
  );
});

describe("reporting input authority", () => {
  it("does not expose editing or independent review without projected authority", () => {
    render(
      <ReportingSetup
        setup={create(HostReportingSetupSchema, {
          input: { revision: 3n, configuration: { timezone: "Africa/Nairobi" } },
        })}
        signal={new AbortController().signal}
        onChanged={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: "Edit reporting configuration" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Review input revision" })).not.toBeInTheDocument();
    expect(screen.getByText(/Africa\/Nairobi/)).toBeInTheDocument();
  });
});

it.each([
  ["loading", { sites: undefined, sitesSettled: false }, "Loading site choices…"],
  ["empty", { sites: [], sitesSettled: true }, "No available site choices."],
  ["error", { sites: [], sitesSettled: true, sitesError: "Catalog unavailable" }, "Catalog unavailable"],
] satisfies [string, Partial<SitesContextValue>, string][])(
  "surfaces %s site choices in reporting permissions",
  async (_state, overrides, text) => {
    const retry = vi.fn();
    const catalog: SitesContextValue = {
      sites: overrides.sites,
      sitesError: "sitesError" in overrides ? overrides.sitesError : null,
      sitesLoaded: false,
      sitesSettled: overrides.sitesSettled,
      sitesPermissionDenied: false,
      siteCatalogAccessGranted: false,
      refetchSites: retry,
      registerSitesPoll: () => () => {},
    };
    const user = userEvent.setup();
    render(
      <SitesContext.Provider value={catalog}>
        <ReportingSetup
          setup={create(HostReportingSetupSchema, { canAdminister: true })}
          signal={new AbortController().signal}
          onChanged={vi.fn()}
        />
      </SitesContext.Provider>,
    );
    await user.click(screen.getByRole("button", { name: "Reporting permissions" }));
    const form = within(screen.getByRole("dialog", { name: "Reporting permissions" }));
    expect(form.getByText(text)).toBeInTheDocument();
    if ("sitesError" in overrides && overrides.sitesError) {
      await user.click(form.getByRole("button", { name: "Retry site choices" }));
      expect(retry).toHaveBeenCalledTimes(1);
    }
  },
);

it("loads only the selected register and follows the server's byte-shortened cursor", async () => {
  const user = userEvent.setup();
  const page = vi.spyOn(hostedAccountsClient, "listHostReportingFacts").mockImplementation(async (request) =>
    create(ListHostReportingFactsResponseSchema, {
      revision: 1n,
      facts: [{ value: { case: "coverage", value: { id: request.cursor ? "fact-50" : "fact-0" } } }],
      nextCursor: request.cursor ? "" : "short-page",
    }),
  );
  render(
    <ReportingSetup
      setup={create(HostReportingSetupSchema, {
        accountId: "account",
        canCreate: true,
        input: {
          revision: 1n,
          configuration: { timezone: "UTC" },
          registerCounts: [{ kind: ReportingFactKind.COVERAGE, count: 51n }],
        },
      })}
      signal={new AbortController().signal}
      onChanged={vi.fn()}
    />,
  );
  expect(await screen.findByText("fact-0")).toBeInTheDocument();
  expect(page).toHaveBeenCalledTimes(1);
  expect(page.mock.calls[0][0]).toMatchObject({
    kind: ReportingFactKind.COVERAGE,
    expectedRevision: 1n,
    cursor: "",
    pageSize: 50,
  });
  await user.click(screen.getByRole("button", { name: "Next page" }));
  expect(await screen.findByText("fact-50")).toBeInTheDocument();
  expect(screen.queryByText("fact-0")).not.toBeInTheDocument();
  expect(page.mock.calls[1][0]).toMatchObject({ cursor: "short-page" });
});

const initialPermissions = useFleetStore.getState().auth.permissions;
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  useFleetStore.setState((state) => ({ auth: { ...state.auth, permissions: initialPermissions } }));
});
const memberSetup = (canManageRepresentatives: boolean) =>
  create(HostReportingSetupSchema, {
    accountId: "selected-client",
    canCreate: true,
    canAdminister: true,
    canManageRepresentatives,
    owners: [{ userId: "preparer", username: "Preparer" }],
    authorities: [{ userId: "publisher", username: "Publisher" }],
    input: {
      revision: 3n,

      configuration: {
        timezone: "UTC",
        representatives: [{ userId: "existing", sourceRef: "Approved representative" }],
      },
    },
  });
it("uses only the selected account members for representative choices", async () => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
  const user = userEvent.setup();
  const fetcher = vi.fn(async (_input: Parameters<typeof fetch>[0], init: NonNullable<Parameters<typeof fetch>[1]>) => {
    expect(new Headers(init.headers).get("X-Fleet-Account-ID")).toBe("selected-client");
    expect(
      JSON.parse(typeof init.body === "string" ? init.body : new TextDecoder().decode(init.body as Uint8Array)),
    ).toEqual({ accountId: "selected-client" });
    return new Response(JSON.stringify({ members: [{ userId: "team-member", username: "Account teammate" }] }), {
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetcher);
  render(<ReportingSetup setup={memberSetup(true)} signal={new AbortController().signal} onChanged={vi.fn()} />);
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  await user.click(screen.getByRole("button", { name: "Edit reporting configuration" }));
  const form = within(screen.getByRole("dialog", { name: "Edit reporting configuration" }));
  await user.click(form.getByText("Authorized representatives"));
  await waitFor(() => expect(form.getByRole("button", { name: "Add representative" })).toBeEnabled());
  await user.click(form.getByRole("button", { name: "Add representative" }));
  const choices = form.getAllByRole("button", { name: "Representative" });
  await user.click(choices[1]);
  expect(await screen.findByRole("option", { name: "Account teammate" })).toBeInTheDocument();
  expect(screen.queryByRole("option", { name: "Preparer" })).not.toBeInTheDocument();
  expect(screen.queryByRole("option", { name: "Publisher" })).not.toBeInTheDocument();
});
it("preserves representatives while a preparer without team management edits other configuration", async () => {
  const user = userEvent.setup();
  useFleetStore.setState((state) => ({ auth: { ...state.auth, permissions: ["user:read"] } }));
  vi.spyOn(authClient, "listUsers").mockResolvedValue(
    create(ListUsersResponseSchema, { users: [{ userId: "organization-user", username: "Organization user" }] }),
  );
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  const setup = memberSetup(false);
  const proposal = vi
    .spyOn(hostedAccountsClient, "mutateHostReportingInputs")
    .mockResolvedValue(create(MutateHostReportingInputsResponseSchema));
  render(<ReportingSetup setup={setup} signal={new AbortController().signal} onChanged={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Edit reporting configuration" }));
  const form = within(screen.getByRole("dialog", { name: "Edit reporting configuration" }));
  await user.click(form.getByText("Authorized representatives"));
  expect(form.getByRole("button", { name: "Representative" })).toBeDisabled();
  expect(form.getByRole("button", { name: "Add representative" })).toBeDisabled();
  await user.type(form.getByLabelText("Contract evidence reference"), "Updated contract evidence");
  await user.type(form.getByLabelText("Change evidence reference"), "Reviewed update");
  await user.click(form.getByRole("button", { name: "Save inputs" }));
  await waitFor(() => expect(proposal).toHaveBeenCalled());
  expect(proposal.mock.calls[0][0].configuration).toMatchObject({
    contractRef: "Updated contract evidence",
    representatives: setup.input!.configuration!.representatives,
  });
  expect(fetcher).not.toHaveBeenCalled();
});
it("drops pending representative edits after live team-management authority is removed", async () => {
  const user = userEvent.setup();
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ members: [{ userId: "team-member", username: "Account teammate" }] }), {
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
  const setup = memberSetup(true);
  const signal = new AbortController().signal;
  const changed = vi.fn();
  const proposal = vi
    .spyOn(hostedAccountsClient, "mutateHostReportingInputs")
    .mockResolvedValue(create(MutateHostReportingInputsResponseSchema));
  const view = render(<ReportingSetup setup={setup} signal={signal} onChanged={changed} />);
  await user.click(screen.getByRole("button", { name: "Edit reporting configuration" }));
  const form = within(screen.getByRole("dialog", { name: "Edit reporting configuration" }));
  await user.click(form.getByText("Authorized representatives"));
  await waitFor(() => expect(form.getByRole("button", { name: "Add representative" })).toBeEnabled());
  await user.click(form.getByRole("button", { name: "Add representative" }));
  view.rerender(
    <ReportingSetup setup={{ ...setup, canManageRepresentatives: false }} signal={signal} onChanged={changed} />,
  );
  expect(form.getByRole("button", { name: "Add representative" })).toBeDisabled();
  await user.type(form.getByLabelText("Contract evidence reference"), "Revised contract");
  await user.type(form.getByLabelText("Change evidence reference"), "Evidence");
  await user.click(form.getByRole("button", { name: "Save inputs" }));
  await waitFor(() => expect(proposal).toHaveBeenCalled());
  expect(proposal.mock.calls[0][0].configuration?.representatives).toEqual(setup.input!.configuration!.representatives);
});

it("offers assumed defaults as a short form and saves them without an evidence reference", async () => {
  const user = userEvent.setup();
  const facts = vi.mocked(hostedAccountsClient.listHostReportingFacts);
  const proposal = vi
    .spyOn(hostedAccountsClient, "mutateHostReportingInputs")
    .mockResolvedValue(create(MutateHostReportingInputsResponseSchema));
  const setup = create(HostReportingSetupSchema, {
    accountId: "selected-client",
    canCreate: true,
    input: { configuration: { timezone: "America/Chicago", reportingYearMonth: 1, reportingYearDay: 1 } },
  });
  render(<ReportingSetup setup={setup} signal={new AbortController().signal} onChanged={vi.fn()} />);
  expect(screen.getByText(/America\/Chicago.*Assumed from this account's sites/)).toBeInTheDocument();
  expect(screen.queryByText(/^Evidence:/)).not.toBeInTheDocument();
  expect(screen.queryByText(/pending/)).not.toBeInTheDocument();
  expect(facts).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Edit reporting configuration" }));
  const form = within(screen.getByRole("dialog", { name: "Edit reporting configuration" }));
  expect(form.getByRole("button", { name: "Reporting timezone" })).toHaveTextContent("America/Chicago");
  expect(form.queryByLabelText("Contract evidence reference")).not.toBeInTheDocument();
  expect(form.queryByLabelText("Change evidence reference")).not.toBeInTheDocument();
  expect(form.queryByText("Prerequisites")).not.toBeInTheDocument();
  await user.click(form.getByRole("button", { name: "Save inputs" }));
  await waitFor(() => expect(proposal).toHaveBeenCalledTimes(1));
  expect(proposal.mock.calls[0][0]).toMatchObject({ expectedRevision: 0n, sourceRef: "" });
  expect(proposal.mock.calls[0][0].configuration).toMatchObject({ timezone: "America/Chicago", prerequisites: [] });
});

it("requires an evidence reference once contract evidence is added", async () => {
  const user = userEvent.setup();
  const setup = create(HostReportingSetupSchema, {
    accountId: "selected-client",
    canCreate: true,
    input: { revision: 2n, configuration: { timezone: "UTC", reportingYearMonth: 1, reportingYearDay: 1 } },
  });
  render(<ReportingSetup setup={setup} signal={new AbortController().signal} onChanged={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Edit reporting configuration" }));
  const form = within(screen.getByRole("dialog", { name: "Edit reporting configuration" }));
  await user.click(form.getByRole("button", { name: "Add contract evidence" }));
  expect(form.getByText("Prerequisites")).toBeInTheDocument();
  expect(form.getByRole("button", { name: "Save inputs" })).toBeEnabled();
  await user.type(form.getByLabelText("Contract evidence reference"), "MSA-12");
  expect(form.getByRole("button", { name: "Save inputs" })).toBeDisabled();
  await user.type(form.getByLabelText("Change evidence reference"), "Signed agreement");
  expect(form.getByRole("button", { name: "Save inputs" })).toBeEnabled();
});

it("saves configuration alone against the revision captured when its form opened", async () => {
  const user = userEvent.setup();
  const proposal = vi
    .spyOn(hostedAccountsClient, "mutateHostReportingInputs")
    .mockResolvedValue(create(MutateHostReportingInputsResponseSchema));
  const setup = memberSetup(false);
  const signal = new AbortController().signal;
  const changed = vi.fn();
  const view = render(<ReportingSetup setup={setup} signal={signal} onChanged={changed} />);
  await user.click(screen.getByRole("button", { name: "Edit reporting configuration" }));
  const form = within(screen.getByRole("dialog", { name: "Edit reporting configuration" }));
  await user.type(form.getByLabelText("Change evidence reference"), "Reviewed draft");
  view.rerender(
    <ReportingSetup
      setup={{ ...setup, input: { ...setup.input!, revision: 4n } }}
      signal={signal}
      onChanged={changed}
    />,
  );
  await user.click(form.getByRole("button", { name: "Save inputs" }));
  await waitFor(() => expect(proposal).toHaveBeenCalledTimes(1));
  expect(proposal.mock.calls[0][0]).toMatchObject({ expectedRevision: 3n, sourceRef: "Reviewed draft" });
  expect(proposal.mock.calls[0][0].mutations).toBeUndefined();
});

it("retains a single fact draft and its page revision across metadata polling", async () => {
  const user = userEvent.setup();
  vi.spyOn(hostedAccountsClient, "listHostReportingFacts").mockImplementation(async (request) =>
    create(ListHostReportingFactsResponseSchema, {
      revision: request.expectedRevision,
      facts: [{ value: { case: "coverage", value: { id: "draft-fact", sourceRef: "Original" } } }],
    }),
  );
  const proposal = vi
    .spyOn(hostedAccountsClient, "mutateHostReportingInputs")
    .mockResolvedValue(create(MutateHostReportingInputsResponseSchema));
  const setup = create(HostReportingSetupSchema, {
    accountId: "fact-draft",
    canCreate: true,
    input: { revision: 3n, configuration: { timezone: "UTC" } },
  });
  const signal = new AbortController().signal;
  const changed = vi.fn();
  const view = render(<ReportingSetup setup={setup} signal={signal} onChanged={changed} />);
  await user.click(await screen.findByRole("button", { name: "Edit fact draft-fact" }));
  const form = within(screen.getByRole("dialog", { name: "Edit fact" }));
  await user.type(form.getByLabelText("Change evidence reference"), "Reviewed row");
  view.rerender(
    <ReportingSetup
      setup={{ ...setup, input: { ...setup.input!, revision: 4n } }}
      signal={signal}
      onChanged={changed}
    />,
  );
  expect(form.getByLabelText("Change evidence reference")).toHaveValue("Reviewed row");
  await user.click(form.getByRole("button", { name: "Save fact" }));
  await waitFor(() => expect(proposal).toHaveBeenCalledTimes(1));
  expect(proposal.mock.calls[0][0]).toMatchObject({
    expectedRevision: 3n,
    mutations: [{ kind: ReportingFactKind.COVERAGE, id: "draft-fact", change: { case: "upsert" } }],
  });
  expect(proposal.mock.calls[0][0].configuration).toBeUndefined();
});

it("requires explicit confirmation and sends only the selected deletion", async () => {
  const user = userEvent.setup();
  vi.spyOn(hostedAccountsClient, "listHostReportingFacts").mockResolvedValue(
    create(ListHostReportingFactsResponseSchema, {
      revision: 5n,
      facts: [{ value: { case: "coverage", value: { id: "delete-me" } } }],
    }),
  );
  const proposal = vi
    .spyOn(hostedAccountsClient, "mutateHostReportingInputs")
    .mockResolvedValue(create(MutateHostReportingInputsResponseSchema));
  render(
    <ReportingSetup
      setup={create(HostReportingSetupSchema, {
        accountId: "fact-delete",
        canCreate: true,
        input: { revision: 5n, configuration: { timezone: "UTC" } },
      })}
      signal={new AbortController().signal}
      onChanged={vi.fn()}
    />,
  );
  await user.click(await screen.findByRole("button", { name: "Delete fact delete-me" }));
  expect(proposal).not.toHaveBeenCalled();
  const form = within(screen.getByRole("dialog", { name: "Delete fact" }));
  await user.type(form.getByLabelText("Change evidence reference"), "Remove duplicate");
  await user.click(form.getByRole("button", { name: "Delete fact" }));
  await waitFor(() => expect(proposal).toHaveBeenCalledTimes(1));
  expect(proposal.mock.calls[0][0]).toMatchObject({
    expectedRevision: 5n,
    mutations: [{ id: "delete-me", change: { case: "delete", value: true } }],
  });
});

it("cancels an import after the in-flight batch confirms and shows committed progress", async () => {
  const user = userEvent.setup();
  let finish: () => void = () => {};
  const proposal = vi.spyOn(hostedAccountsClient, "mutateHostReportingInputs").mockImplementation(async (request) => {
    if (proposal.mock.calls.length === 3)
      await new Promise<void>((resolve) => {
        finish = resolve;
      });
    return create(MutateHostReportingInputsResponseSchema, { input: { revision: request.expectedRevision! + 1n } });
  });
  render(
    <ReportingSetup
      setup={create(HostReportingSetupSchema, {
        accountId: "import-cancel",
        canCreate: true,
        input: { revision: 1n, configuration: { timezone: "UTC" } },
      })}
      signal={new AbortController().signal}
      onChanged={vi.fn()}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import facts" }));
  const form = within(screen.getByRole("dialog", { name: "Add or update reporting facts" }));
  await user.upload(
    form.getByLabelText("Facts file (CSV or JSON)"),
    new File([JSON.stringify(Array.from({ length: 601 }, (_, i) => ({ id: `c-${i}` })))], "facts.json", {
      type: "application/json",
    }),
  );
  await user.type(form.getByLabelText("Import evidence reference"), "Reviewed import");
  await user.click(form.getByRole("button", { name: "Add or update rows" }));
  await waitFor(() => expect(proposal).toHaveBeenCalledTimes(3));
  await user.click(form.getByRole("button", { name: "Cancel import" }));
  finish();
  expect(await form.findByText("Import stopped. Confirmed batches remain saved.")).toBeInTheDocument();
  expect(form.getByRole("status")).toHaveTextContent("600 rows confirmed committed · Revision 4.");
  expect(proposal).toHaveBeenCalledTimes(3);
});

it("stops before submitting when the browser cannot retain an import batch for recovery", async () => {
  const user = userEvent.setup();
  const proposal = vi
    .spyOn(hostedAccountsClient, "mutateHostReportingInputs")
    .mockResolvedValue(create(MutateHostReportingInputsResponseSchema));
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new DOMException("Quota exceeded", "QuotaExceededError");
  });
  render(
    <ReportingSetup
      setup={create(HostReportingSetupSchema, {
        accountId: "import-storage",
        canCreate: true,
        input: { revision: 1n, configuration: { timezone: "UTC" } },
      })}
      signal={new AbortController().signal}
      onChanged={vi.fn()}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import facts" }));
  const form = within(screen.getByRole("dialog", { name: "Add or update reporting facts" }));
  await user.upload(
    form.getByLabelText("Facts file (CSV or JSON)"),
    new File(['[{"id":"not-submitted"}]'], "facts.json", { type: "application/json" }),
  );
  await user.type(form.getByLabelText("Import evidence reference"), "Reviewed");
  await user.click(form.getByRole("button", { name: "Add or update rows" }));
  expect(await form.findByText(/browser could not retain the submitted rows/)).toBeInTheDocument();
  expect(proposal).not.toHaveBeenCalled();
});

it("stops scheduling import batches when the authentication session changes", async () => {
  const user = userEvent.setup();
  const originalAuth = useFleetStore.getState().auth;
  const onChanged = vi.fn();
  const proposal = vi.spyOn(hostedAccountsClient, "mutateHostReportingInputs").mockImplementation(async (request) => {
    act(() =>
      useFleetStore.setState({ auth: { ...originalAuth, sessionGeneration: originalAuth.sessionGeneration + 1 } }),
    );
    return create(MutateHostReportingInputsResponseSchema, { input: { revision: request.expectedRevision! + 1n } });
  });
  try {
    render(
      <ReportingSetup
        setup={create(HostReportingSetupSchema, {
          accountId: "import-session-change",
          canCreate: true,
          input: { revision: 1n, configuration: { timezone: "UTC" } },
        })}
        signal={new AbortController().signal}
        onChanged={onChanged}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Import facts" }));
    const form = within(screen.getByRole("dialog", { name: "Add or update reporting facts" }));
    await user.upload(
      form.getByLabelText("Facts file (CSV or JSON)"),
      new File([JSON.stringify(Array.from({ length: 201 }, (_, i) => ({ id: `session-${i}` })))], "facts.json", {
        type: "application/json",
      }),
    );
    await user.type(form.getByLabelText("Import evidence reference"), "Reviewed import");
    await user.click(form.getByRole("button", { name: "Add or update rows" }));
    await waitFor(() => expect(proposal).toHaveBeenCalledTimes(1));
    expect(onChanged).not.toHaveBeenCalled();
    expect(Object.values(sessionStorage).join(" ")).not.toContain("session-0");
  } finally {
    act(() => useFleetStore.setState({ auth: originalAuth }));
  }
});
