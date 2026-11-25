import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import ConfigurationLibrary from "./ConfigurationLibrary";
import { ConfigurationSchema } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

const mocks = vi.hoisted(() => ({
  client: { listConfigurations: vi.fn(), listOperations: vi.fn(), setDefaultConfiguration: vi.fn() },
  manage: true,
  readOnly: false,
}));
vi.mock("@/shared/hooks/useWindowDimensions", () => ({ useWindowDimensions: () => ({ isPhone: false }) }));
vi.mock("./api", () => ({
  usePoolConfigurationClient: () => ({ client: mocks.client, accountId: "customer", readOnly: mocks.readOnly }),
  usePoolConfigurationPermissions: () => ({ manage: mocks.manage, change: true }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.manage = true;
  mocks.readOnly = false;
  mocks.client.listConfigurations.mockResolvedValue({
    configurations: [
      create(ConfigurationSchema, { configurationId: "first", name: "Primary", version: 2n, isDefault: true }),
      create(ConfigurationSchema, { configurationId: "second", name: "Alternative", version: 3n }),
    ],
    nextPageToken: "",
  });
  mocks.client.setDefaultConfiguration.mockResolvedValue({});
  mocks.client.listOperations.mockResolvedValue({ operations: [], nextPageToken: "" });
});

it("shows the default and changes it using the selected account and version", async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <ConfigurationLibrary />
    </MemoryRouter>,
  );
  expect(await screen.findByText("Default")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Actions for Alternative" }));
  expect(screen.queryByRole("dialog", { name: "Pool configuration" })).not.toBeInTheDocument();
  await user.click(screen.getByText("Set as default"));
  const dialog = await screen.findByRole("dialog", { name: "Set default configuration" });
  await user.click(within(dialog).getByRole("button", { name: "Set as default" }));
  await waitFor(() =>
    expect(mocks.client.setDefaultConfiguration).toHaveBeenCalledWith({
      accountId: "customer",
      configurationId: "second",
      expectedVersion: 3n,
    }),
  );
  await waitFor(() => expect(mocks.client.listConfigurations).toHaveBeenCalledTimes(2));
});

it("lets library readers open details from the assigned count without exposing management controls", async () => {
  const user = userEvent.setup();
  mocks.manage = false;
  render(
    <MemoryRouter>
      <ConfigurationLibrary />
    </MemoryRouter>,
  );
  expect(await screen.findByText("Default")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Actions for Alternative" })).not.toBeInTheDocument();
  const row = screen.getByRole("row", { name: /Alternative/ });
  await user.click(within(row).getByRole("cell", { name: "0" }));
  const details = await screen.findByRole("dialog", { name: "Pool configuration" });
  expect(await within(details).findByText("No equipment updates yet.")).toBeInTheDocument();
  expect(mocks.client.listOperations).toHaveBeenCalledWith(
    expect.objectContaining({ configurationId: "second" }),
    expect.anything(),
  );
});

it("provides one details tab stop per configuration row", async () => {
  const user = userEvent.setup();
  mocks.manage = false;
  render(
    <MemoryRouter>
      <ConfigurationLibrary />
    </MemoryRouter>,
  );
  const first = await screen.findByRole("row", { name: /View configuration details for Primary/ });
  const second = screen.getByRole("row", { name: /View configuration details for Alternative/ });

  await user.tab();
  expect(first).toHaveFocus();
  await user.tab();
  expect(second).toHaveFocus();
  await user.keyboard("{Enter}");

  const details = await screen.findByRole("dialog", { name: "Pool configuration" });
  expect(within(details).getByText("Alternative")).toBeInTheDocument();
});
