import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { ConfigurationSelector } from "./ConfigurationSelector";

const api = vi.hoisted(() => ({ listConfigurations: vi.fn(), getAssignments: vi.fn(), removal: vi.fn() }));
vi.mock("./api", () => ({
  usePoolConfigurationClient: () => ({ client: api, accountId: "", readOnly: false }),
  usePoolConfigurationPermissions: () => ({ read: true, manage: true, change: true }),
}));
vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: ({ onAuthenticated }: { onAuthenticated: (username: string, password: string) => void }) => (
    <button onClick={() => onAuthenticated("operator", "password")}>Authenticate</button>
  ),
}));
vi.mock("./RemoveConfigurationDialog", () => ({
  RemoveConfigurationDialog: (props: unknown) => {
    api.removal(props);
    return <div>Review assignment removal</div>;
  },
}));
beforeEach(() => {
  vi.clearAllMocks();
  api.listConfigurations.mockResolvedValue({ configurations: [], nextPageToken: "" });
  api.getAssignments.mockResolvedValue({
    assignments: [{ configurationId: "original", deviceId: 1n, deviceIdentifier: "ct1" }],
    selectedCount: 1n,
  });
});
it("offers reviewed removal instead of a new whole-container assignment after activation", async () => {
  render(<ConfigurationSelector deviceIdentifiers={["ct1"]} removalOnly onDismiss={vi.fn()} />);
  fireEvent.click(screen.getByRole("button", { name: "Authenticate" }));
  await screen.findByText("Review assignment removal");
  expect(api.removal).toHaveBeenCalledWith(
    expect.objectContaining({
      assignments: [expect.objectContaining({ configurationId: "original" })],
      password: "password",
      assignmentOnly: true,
    }),
  );
  expect(api.listConfigurations).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "Assign" })).not.toBeInTheDocument();
});
it("does not offer removal or assignment when the container assignment is already retired", async () => {
  api.getAssignments.mockResolvedValue({ assignments: [{ configurationId: "", deviceId: 1n }], selectedCount: 1n });
  render(<ConfigurationSelector deviceIdentifiers={["ct1"]} removalOnly onDismiss={vi.fn()} />);
  fireEvent.click(screen.getByRole("button", { name: "Authenticate" }));
  await screen.findByText(
    "No whole-container pool assignment remains. Manage pool configurations from the hashboards.",
  );
  expect(api.removal).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "Assign" })).not.toBeInTheDocument();
});
