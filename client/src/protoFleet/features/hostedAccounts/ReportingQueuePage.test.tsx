import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { hostedAccountsClient } from "./api";
import ReportingQueuePage from "./ReportingQueuePage";
import { sitesClient } from "@/protoFleet/api/clients";
import {
  GetHostReportingSetupResponseSchema,
  ListHostReportingAccountsResponseSchema,
  ListHostReportQueueResponseSchema,
  SetHostReportingAuthorityResponseSchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { ListSitesResponseSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useFleetStore } from "@/protoFleet/store";

vi.mock("./AccountLayout", () => ({ AccountLayout: ({ children }: { children: ReactNode }) => children }));
const initialAuth = useFleetStore.getState().auth;
afterEach(() => {
  vi.restoreAllMocks();
  useFleetStore.setState({ auth: initialAuth });
});

it("loads the allowed site catalog on the fullscreen reporting page and saves a granular first grant", async () => {
  const user = userEvent.setup();
  useFleetStore.setState({ auth: { ...initialAuth, permissions: ["site:read"] } });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
  const sites = vi
    .spyOn(sitesClient, "listSites")
    .mockResolvedValue(create(ListSitesResponseSchema, { sites: [{ site: { id: 7n, name: "Allowed site" } }] }));
  vi.spyOn(hostedAccountsClient, "listHostReportingAccounts").mockResolvedValue(
    create(ListHostReportingAccountsResponseSchema, {
      accounts: [{ accountId: "client", name: "Northstar", canAdminister: true }],
    }),
  );
  vi.spyOn(hostedAccountsClient, "listHostReportQueue").mockResolvedValue(create(ListHostReportQueueResponseSchema));
  vi.spyOn(hostedAccountsClient, "getHostReportingSetup").mockResolvedValue(
    create(GetHostReportingSetupResponseSchema, {
      setup: { accountId: "client", canAdminister: true, owners: [{ userId: "operator", username: "Casey" }] },
    }),
  );
  const save = vi
    .spyOn(hostedAccountsClient, "setHostReportingAuthority")
    .mockResolvedValue(create(SetHostReportingAuthorityResponseSchema));
  render(
    <MemoryRouter initialEntries={["/accounts/reports?account=client"]}>
      <ReportingQueuePage />
    </MemoryRouter>,
  );
  await user.click(await screen.findByRole("button", { name: "Reporting permissions" }));
  const form = within(screen.getByRole("dialog", { name: "Reporting permissions" }));
  await user.click(form.getByRole("button", { name: "Operator" }));
  await user.click(await screen.findByRole("option", { name: "Casey" }));
  // An owner without a stored grant is a manager with implicit all-site authority.
  expect(form.getByLabelText("Create reports")).toBeChecked();
  expect(form.getByRole("radio", { name: "All account sites" })).toBeChecked();
  await user.click(form.getByRole("radio", { name: "Specific sites" }));
  await user.click(await form.findByLabelText("Allowed site"));
  await user.click(form.getByRole("button", { name: "Save reporting permissions" }));
  await waitFor(() =>
    expect(save).toHaveBeenCalledWith(
      {
        accountId: "client",
        authority: { userId: "operator", canCreate: true, allSites: false, siteIds: [7n] },
        expectedRevision: 0n,
      },
      expect.anything(),
    ),
  );
  expect(sites).toHaveBeenCalledTimes(1);
});
