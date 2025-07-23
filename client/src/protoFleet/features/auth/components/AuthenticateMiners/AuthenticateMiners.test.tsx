import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import AuthenticateMiners from "./AuthenticateMiners";
import { MinerStateSnapshotSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useFleet from "@/protoFleet/api/useFleet";
import { useFleetMiners } from "@/protoFleet/features/fleetManagement/store/useFleetStore";

vi.mock("@/protoFleet/api/useFleet");
vi.mock("@/protoFleet/features/fleetManagement/store/useFleetStore");
vi.mock("@/shared/features/toaster");

const mockMiners = [
  { deviceIdentifier: "miner1", macAddress: "00:00:00:00:00:01" },
  { deviceIdentifier: "miner2", macAddress: "00:00:00:00:00:02" },
  { deviceIdentifier: "miner3", macAddress: "00:00:00:00:00:03" },
];

const mockOnClose = vi.fn();

beforeEach(() => {
  vi.mocked(useFleet).mockReturnValue({
    minerIds: mockMiners.map((m) => m.deviceIdentifier),
    hasMore: false,
    isLoading: false,
    setFilter: vi.fn(),
    loadMore: vi.fn(),
  });
  vi.mocked(useFleetMiners).mockReturnValue(
    mockMiners.map((miner) => create(MinerStateSnapshotSchema, miner)),
  );
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
    const { getByText } = render(<AuthenticateMiners onClose={mockOnClose} />);

    fireEvent.click(getByText(showMinersLabel));

    expect(getByText("3 miners selected")).toBeInTheDocument();
  });

  it("toggles between showing and hiding miner list", () => {
    const { getByText, queryByText } = render(
      <AuthenticateMiners onClose={mockOnClose} />,
    );

    expect(queryByText("MAC address")).not.toBeInTheDocument();

    fireEvent.click(getByText(showMinersLabel));
    expect(getByText("MAC address")).toBeInTheDocument();

    fireEvent.click(getByText("Hide miner list"));
    expect(queryByText("MAC address")).not.toBeInTheDocument();
  });

  it("allows entering bulk credentials", async () => {
    const { getByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} />,
    );

    const usernameInput = getByLabelText(bulkUsernameLabel);
    const passwordInput = getByLabelText(bulkPasswordLabel);

    fireEvent.change(usernameInput, { target: { value: mockUsername } });
    fireEvent.change(passwordInput, { target: { value: mockPassword } });

    expect(usernameInput).toHaveValue(mockUsername);
    expect(passwordInput).toHaveValue(mockPassword);
  });

  it("shows error when authenticating without credentials", () => {
    const { getByText } = render(<AuthenticateMiners onClose={mockOnClose} />);

    fireEvent.click(getByText("Authenticate"));

    expect(
      getByText("Enter a username and password and try again."),
    ).toBeInTheDocument();
  });

  it("shows individual credential inputs for each miner", async () => {
    const { getByText, getAllByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} />,
    );

    fireEvent.click(getByText(showMinersLabel));

    const usernameInputs = getAllByLabelText(usernameLabel);
    const passwordInputs = getAllByLabelText(passwordLabel);

    expect(usernameInputs).toHaveLength(mockMiners.length);
    expect(passwordInputs).toHaveLength(mockMiners.length);
  });

  it("populates individual miner inputs with bulk credentials", async () => {
    const { getByText, getByLabelText, getAllByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} />,
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
      <AuthenticateMiners onClose={mockOnClose} />,
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
    const { getByText } = render(<AuthenticateMiners onClose={mockOnClose} />);

    fireEvent.click(getByText(showMinersLabel));

    fireEvent.click(getByText("Select none"));
    expect(getByText("0 miners selected")).toBeInTheDocument();

    fireEvent.click(getByText("Select all"));
    expect(getByText("3 miners selected")).toBeInTheDocument();
  });

  it("filters miners by model", async () => {
    const { getByText } = render(<AuthenticateMiners onClose={mockOnClose} />);

    fireEvent.click(getByText(showMinersLabel));

    fireEvent.click(getByText("Model"));

    const protoRigOption = getByText("Proto Rig");
    expect(protoRigOption).toBeInTheDocument();
  });

  it("disables inputs during authentication", async () => {
    const { getByText, getByLabelText } = render(
      <AuthenticateMiners onClose={mockOnClose} />,
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
      <AuthenticateMiners onClose={mockOnClose} />,
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
});
