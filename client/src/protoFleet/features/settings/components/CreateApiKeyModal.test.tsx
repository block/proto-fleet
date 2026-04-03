import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import CreateApiKeyModal from "./CreateApiKeyModal";
import { useApiKeys } from "@/protoFleet/api/useApiKeys";
import type { ApiKeyItem } from "@/protoFleet/api/useApiKeys";
import * as utility from "@/shared/utils/utility";

vi.mock("@/protoFleet/api/useApiKeys");
vi.mock("@/shared/features/toaster");
vi.mock("@/shared/utils/utility", async () => {
  const actual = await vi.importActual<typeof utility>("@/shared/utils/utility");
  return {
    ...actual,
    copyToClipboard: vi.fn().mockResolvedValue(undefined),
  };
});

const mockCreateApiKey = vi.fn();
const mockOnDismiss = vi.fn();
const mockOnSuccess = vi.fn();

const mockApiKeyInfo: ApiKeyItem = {
  keyId: "key-123",
  name: "CI integration",
  prefix: "pf_test",
  createdAt: new Date("2026-04-03T12:00:00Z"),
  expiresAt: null,
  lastUsedAt: null,
  createdBy: "admin",
};

beforeEach(() => {
  vi.mocked(useApiKeys).mockReturnValue({
    createApiKey: mockCreateApiKey,
    listApiKeys: vi.fn(),
    revokeApiKey: vi.fn(),
  });

  vi.clearAllMocks();
});

describe("CreateApiKeyModal", () => {
  it("renders the create step", () => {
    const { getByLabelText, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    expect(getByText("Create API key")).toBeInTheDocument();
    expect(getByLabelText("Key name")).toBeInTheDocument();
    expect(getByLabelText("Expiration date (optional)")).toBeInTheDocument();
    expect(getByText("Create")).toBeInTheDocument();
  });

  it("shows a validation error when the name is empty", async () => {
    const { getByLabelText, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "   " } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(getByText("Name is required")).toBeInTheDocument();
    });
  });

  it("calls createApiKey with a trimmed name", async () => {
    mockCreateApiKey.mockImplementation(({ onSuccess }) => {
      onSuccess("pf_test_secret", mockApiKeyInfo);
    });

    const { getByLabelText, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "  CI integration  " } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(mockCreateApiKey).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "CI integration",
        }),
      );
    });
  });

  it("renders the standard success dialog after creation", async () => {
    mockCreateApiKey.mockImplementation(({ onSuccess }) => {
      onSuccess("pf_test_secret", mockApiKeyInfo);
    });

    const { getByLabelText, getByText, getByTestId } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(getByText("API key created")).toBeInTheDocument();
      expect(getByText("Copy this key now and store it securely. It won't be shown again.")).toBeInTheDocument();
      expect(getByTestId("api-key-value")).toHaveTextContent("pf_test_secret");
      expect(getByText("Done")).toBeInTheDocument();
    });
  });

  it("allows copying the generated API key", async () => {
    mockCreateApiKey.mockImplementation(({ onSuccess }) => {
      onSuccess("pf_test_secret", mockApiKeyInfo);
    });

    const { getByLabelText, getByText, getByRole } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(getByText("API key created")).toBeInTheDocument();
    });

    fireEvent.click(getByRole("button", { name: /copy api key/i }));
    expect(utility.copyToClipboard).toHaveBeenCalledWith("pf_test_secret");
  });

  it("calls onSuccess and onDismiss when Done is clicked", async () => {
    mockCreateApiKey.mockImplementation(({ onSuccess }) => {
      onSuccess("pf_test_secret", mockApiKeyInfo);
    });

    const { getByLabelText, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(getByText("API key created")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Done"));
    expect(mockOnSuccess).toHaveBeenCalled();
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("ignores a stale create success after the modal is dismissed", async () => {
    let resolveCreate: ((apiKey: string, info: ApiKeyItem) => void) | undefined;
    let finishCreate: (() => void) | undefined;

    mockCreateApiKey.mockImplementation(({ onSuccess, onFinally }) => {
      resolveCreate = onSuccess;
      finishCreate = onFinally;
    });

    const { getByLabelText, getByText, queryByText, queryByTestId, rerender } = render(
      <CreateApiKeyModal open onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(mockCreateApiKey).toHaveBeenCalled();
    });

    rerender(<CreateApiKeyModal open={false} onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />);
    rerender(<CreateApiKeyModal open onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />);

    await act(async () => {
      resolveCreate?.("pf_test_secret", mockApiKeyInfo);
      finishCreate?.();
    });

    expect(queryByText("API key created")).not.toBeInTheDocument();
    expect(queryByTestId("api-key-value")).not.toBeInTheDocument();
    expect(getByText("Create API key")).toBeInTheDocument();
  });
});
