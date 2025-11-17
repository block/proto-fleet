import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AuthenticateMiners from "./AuthenticateMiners";
import { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import useFleet from "@/protoFleet/api/useFleet";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";

vi.mock("@/protoFleet/api/useAuthNeededMiners");
vi.mock("@/protoFleet/api/useFleet");
vi.mock("@/protoFleet/api/useMinerPairing");
vi.mock("@/protoFleet/api/useOnboardedStatus");
vi.mock("@/protoFleet/store");
vi.mock("@/shared/features/toaster");

const mockUnpairedMiners = {
  miner1: {
    deviceIdentifier: "miner1",
    macAddress: "00:00:00:00:00:01",
    model: "Proto Rig",
    name: "Miner 1",
    ipAddress: "192.168.1.101",
  },
  miner2: {
    deviceIdentifier: "miner2",
    macAddress: "00:00:00:00:00:02",
    model: "Proto Rig",
    name: "Miner 2",
    ipAddress: "192.168.1.102",
  },
  miner3: {
    deviceIdentifier: "miner3",
    macAddress: "00:00:00:00:00:03",
    model: "Proto Rig",
    name: "Miner 3",
    ipAddress: "192.168.1.103",
  },
} as unknown as Record<string, MinerStateSnapshot>;

const mockOnClose = vi.fn();
const mockPair = vi.fn();
const mockRefetchOnboardingStatus = vi.fn();
const mockRefetchFleet = vi.fn();
const mockOnSuccess = vi.fn();

beforeEach(() => {
  vi.mocked(useAuthNeededMiners).mockReturnValue({
    minerIds: ["miner1", "miner2", "miner3"],
    miners: mockUnpairedMiners,
    totalMiners: 3,
    hasMore: false,
    isLoading: false,
    setFilter: vi.fn(),
    loadMore: vi.fn(),
    refetch: vi.fn(),
  });

  vi.mocked(useMinerPairing).mockReturnValue({
    discover: vi.fn(),
    pair: mockPair,
    discoverPending: false,
    pairingPending: false,
  });

  vi.mocked(useOnboardedStatus).mockReturnValue({
    poolConfigured: false,
    devicePaired: true,
    refetch: mockRefetchOnboardingStatus,
  });

  vi.mocked(useFleet).mockReturnValue({
    minerIds: [],
    miners: {},
    totalMiners: 0,
    hasMore: false,
    isLoading: false,
    setFilter: vi.fn(),
    loadMore: vi.fn(),
    refetch: mockRefetchFleet,
  });

  vi.clearAllMocks();
});

describe("AuthenticateMiners", () => {
  const showMinersLabel = "Show miners";
  const bulkUsernameLabel = "Miner username";
  const bulkPasswordLabel = "Miner password";
  const usernameLabel = "Username";
  const passwordLabel = "Password";

  const mockUsername = "admin";
  const mockPassword = "test1234";

  it("renders with all miners selected by default", () => {
    const { getByText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.click(getByText(showMinersLabel));

    expect(getByText("3 miners selected")).toBeInTheDocument();
  });

  it("toggles between showing and hiding miner list", () => {
    const { getByText, queryByText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    expect(queryByText("IP Address")).not.toBeInTheDocument();

    fireEvent.click(getByText(showMinersLabel));
    expect(getByText("IP Address")).toBeInTheDocument();

    fireEvent.click(getByText("Hide miner list"));
    expect(queryByText("IP Address")).not.toBeInTheDocument();
  });

  it("allows entering bulk credentials", async () => {
    const { getByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText(bulkUsernameLabel);
    const passwordInput = getByLabelText(bulkPasswordLabel);

    fireEvent.change(usernameInput, { target: { value: mockUsername } });
    fireEvent.change(passwordInput, { target: { value: mockPassword } });

    expect(usernameInput).toHaveValue(mockUsername);
    expect(passwordInput).toHaveValue(mockPassword);
  });

  it("shows error when authenticating without credentials", () => {
    const { getByText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.click(getByText("Authenticate"));

    expect(
      getByText("Enter a username and password and try again."),
    ).toBeInTheDocument();
  });

  it("shows individual credential inputs for each miner", async () => {
    const { getByText, getAllByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.click(getByText(showMinersLabel));

    const usernameInputs = getAllByLabelText(usernameLabel);
    const passwordInputs = getAllByLabelText(passwordLabel);

    expect(usernameInputs).toHaveLength(Object.keys(mockUnpairedMiners).length);
    expect(passwordInputs).toHaveLength(Object.keys(mockUnpairedMiners).length);
  });

  it("populates individual miner inputs with bulk credentials", async () => {
    const { getByText, getByLabelText, getAllByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText(bulkUsernameLabel), {
      target: { value: mockUsername },
    });
    fireEvent.change(getByLabelText(bulkPasswordLabel), {
      target: { value: mockPassword },
    });

    fireEvent.click(getByText(showMinersLabel));

    const usernameInputs = getAllByLabelText(usernameLabel);
    const passwordInputs = getAllByLabelText(passwordLabel);

    usernameInputs.forEach((input) => {
      expect(input).toHaveValue(mockUsername);
    });
    passwordInputs.forEach((input) => {
      expect(input).toHaveValue(mockPassword);
    });
  });

  it("toggles password visibility", async () => {
    const { getByText, getByLabelText, getAllByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.click(getByText(showMinersLabel));

    const passwordInputs = getAllByLabelText(passwordLabel);
    passwordInputs.forEach((input) => {
      expect(input).toHaveAttribute("type", "password");
    });

    fireEvent.click(getByLabelText("Show passwords"));

    passwordInputs.forEach((input) => {
      expect(input).toHaveAttribute("type", "text");
    });
  });

  it("allows selecting and deselecting all miners", () => {
    const { getByText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.click(getByText(showMinersLabel));

    fireEvent.click(getByText("Select none"));
    expect(getByText("0 miners selected")).toBeInTheDocument();

    fireEvent.click(getByText("Select all"));
    expect(getByText("3 miners selected")).toBeInTheDocument();
  });

  it("filters miners by model", async () => {
    const { getByText, getAllByText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.click(getByText(showMinersLabel));

    // Find the Model dropdown filter button (not the table header)
    const modelButtons = getAllByText("Model");
    // The dropdown filter button should be the first one
    const modelDropdown = modelButtons[0].closest("button");
    expect(modelDropdown).toBeInTheDocument();

    fireEvent.click(modelDropdown!);

    // Check that Proto Rig option appears (could be multiple - in dropdown and in table)
    const protoRigOptions = getAllByText("Proto Rig");
    expect(protoRigOptions.length).toBeGreaterThan(0);
  });

  it("disables inputs during authentication", async () => {
    const { getByText, getByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText(bulkUsernameLabel), {
      target: { value: mockUsername },
    });
    fireEvent.change(getByLabelText(bulkPasswordLabel), {
      target: { value: mockPassword },
    });

    expect(getByLabelText(bulkUsernameLabel)).not.toBeDisabled();
    expect(getByLabelText(bulkPasswordLabel)).not.toBeDisabled();

    fireEvent.click(getByText("Authenticate"));

    expect(getByLabelText(bulkUsernameLabel)).toBeDisabled();
    expect(getByLabelText(bulkPasswordLabel)).toBeDisabled();
  });

  it("clears individual credentials when toggling miner list", async () => {
    const { getByText, getAllByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.click(getByText(showMinersLabel));

    const firstUsernameInput = getAllByLabelText(usernameLabel)[0];

    fireEvent.change(firstUsernameInput, {
      target: { value: "customuser" },
    });

    fireEvent.click(getByText("Hide miner list"));
    fireEvent.click(getByText(showMinersLabel));

    const usernameInputs = getAllByLabelText(usernameLabel);
    expect(usernameInputs[0]).not.toHaveValue("customuser");
  });

  it("calls pair API with bulk credentials when authenticate is clicked", async () => {
    const { getByText, getByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText(bulkUsernameLabel), {
      target: { value: mockUsername },
    });
    fireEvent.change(getByLabelText(bulkPasswordLabel), {
      target: { value: mockPassword },
    });

    fireEvent.click(getByText("Authenticate"));

    expect(mockPair).toHaveBeenCalledTimes(1);
    expect(mockPair).toHaveBeenCalledWith(
      expect.objectContaining({
        pairRequest: expect.objectContaining({
          deviceIdentifiers: expect.arrayContaining([
            "miner1",
            "miner2",
            "miner3",
          ]),
          credentials: expect.objectContaining({
            username: mockUsername,
            password: mockPassword,
          }),
        }),
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
  });

  it("groups miners with same credentials into single pair request", async () => {
    const { getByText, getByLabelText, getAllByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText(bulkUsernameLabel), {
      target: { value: "bulk-user" },
    });
    fireEvent.change(getByLabelText(bulkPasswordLabel), {
      target: { value: "bulk-pass" },
    });

    fireEvent.click(getByText(showMinersLabel));
    const usernameInputs = getAllByLabelText(usernameLabel);
    const passwordInputs = getAllByLabelText(passwordLabel);

    fireEvent.change(usernameInputs[0], {
      target: { value: "custom-user" },
    });
    fireEvent.change(passwordInputs[0], {
      target: { value: "custom-pass" },
    });

    fireEvent.click(getByText("Authenticate"));

    // Should make 2 pair requests: one for custom credentials, one for bulk
    expect(mockPair).toHaveBeenCalledTimes(2);
  });

  it("calls refetch after successful authentication", async () => {
    const { getByText, getByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    mockPair.mockImplementation(({ onSuccess }) => {
      onSuccess([]);
    });

    fireEvent.change(getByLabelText(bulkUsernameLabel), {
      target: { value: mockUsername },
    });
    fireEvent.change(getByLabelText(bulkPasswordLabel), {
      target: { value: mockPassword },
    });

    fireEvent.click(getByText("Authenticate"));

    await vi.waitFor(() => {
      expect(mockRefetchOnboardingStatus).toHaveBeenCalled();
      expect(mockRefetchFleet).toHaveBeenCalled();
      expect(mockOnSuccess).toHaveBeenCalled();
    });
  });

  it("displays correct total devices count", () => {
    const { getByText } = render(
      <AuthenticateMiners onClose={mockOnClose} onSuccess={mockOnSuccess} />,
    );

    expect(getByText("3 miners remaining")).toBeInTheDocument();
  });
});
