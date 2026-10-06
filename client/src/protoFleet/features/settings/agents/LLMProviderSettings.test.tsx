import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { AgentHarness, LLMProvider } from "@/protoFleet/api/generated/chat/v1/chat_pb";
import LLMProviderSettings from "@/protoFleet/features/settings/agents/LLMProviderSettings";

const mocks = vi.hoisted(() => ({
  discoverModels: vi.fn(),
  getLLMConfig: vi.fn(),
  updateLLMConfig: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  chatClient: mocks,
}));

describe("LLMProviderSettings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.discoverModels.mockResolvedValue({ models: [] });
    mocks.getLLMConfig.mockResolvedValue({
      config: {
        harness: AgentHarness.NATIVE,
        provider: LLMProvider.LLM_PROVIDER_UNSPECIFIED,
        hasApiKey: false,
        baseUrl: "",
        model: "",
        gooseBaseUrl: "",
        hasGooseSecret: false,
        configured: false,
      },
    });
  });

  test("loads remote-provider models after the operator enters an API key", async () => {
    mocks.discoverModels.mockResolvedValue({
      models: [{ id: "gpt-test", displayName: "GPT Test" }],
    });
    render(<LLMProviderSettings />);

    const providerSelect = await screen.findByRole("button", { name: "Model provider" });
    expect(providerSelect).toHaveTextContent("Select a provider");

    fireEvent.click(providerSelect);
    expect(screen.queryByText("Custom OpenAI-compatible")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("OpenAI"));

    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "test-key" } });

    await waitFor(() =>
      expect(mocks.discoverModels).toHaveBeenCalledWith({
        provider: LLMProvider.LLM_PROVIDER_OPENAI,
        apiKey: "test-key",
        baseUrl: "https://api.openai.com/v1",
        useStoredApiKey: false,
      }),
    );

    expect(screen.queryByRole("button", { name: /fetch models/i })).not.toBeInTheDocument();
    const modelSelect = screen.getByRole("button", { name: "Model" });
    expect(modelSelect).toHaveTextContent("Select a model");
    expect(screen.getByRole("button", { name: "Save Minerbot settings" })).toBeDisabled();

    fireEvent.click(modelSelect);
    fireEvent.click(screen.getByText("GPT Test (gpt-test)"));
    expect(screen.getByRole("button", { name: "Save Minerbot settings" })).toBeEnabled();
  });

  test("loads the saved provider models on page load", async () => {
    mocks.getLLMConfig.mockResolvedValue({
      config: {
        harness: AgentHarness.NATIVE,
        provider: LLMProvider.LLM_PROVIDER_ANTHROPIC,
        hasApiKey: true,
        baseUrl: "https://api.anthropic.com",
        model: "claude-saved",
        gooseBaseUrl: "",
        hasGooseSecret: false,
        configured: true,
      },
    });
    mocks.discoverModels.mockResolvedValue({
      models: [
        { id: "claude-new", displayName: "Claude New" },
        { id: "claude-saved", displayName: "Claude Saved" },
      ],
    });

    render(<LLMProviderSettings />);

    await waitFor(() =>
      expect(mocks.discoverModels).toHaveBeenCalledWith({
        provider: LLMProvider.LLM_PROVIDER_ANTHROPIC,
        apiKey: "",
        baseUrl: "https://api.anthropic.com",
        useStoredApiKey: true,
      }),
    );
    expect(screen.getByRole("button", { name: "Model" })).toHaveTextContent("Claude Saved");
  });

  test("loads Ollama models when the provider is selected", async () => {
    mocks.discoverModels.mockResolvedValue({
      models: [{ id: "llama-test:latest", displayName: "llama-test:latest" }],
    });
    render(<LLMProviderSettings />);

    const providerSelect = await screen.findByRole("button", { name: "Model provider" });
    fireEvent.click(providerSelect);
    fireEvent.click(screen.getByText("Ollama"));

    await waitFor(() =>
      expect(mocks.discoverModels).toHaveBeenCalledWith({
        provider: LLMProvider.LLM_PROVIDER_OLLAMA,
        apiKey: "",
        baseUrl: "http://127.0.0.1:11434",
        useStoredApiKey: false,
      }),
    );
    expect(screen.getByRole("button", { name: "Model" })).toHaveTextContent("Select a model");
  });

  test("can remove a stored API key without losing the selected model", async () => {
    mocks.getLLMConfig.mockResolvedValue({
      config: {
        harness: AgentHarness.NATIVE,
        provider: LLMProvider.LLM_PROVIDER_OPENAI,
        hasApiKey: true,
        baseUrl: "https://api.openai.com/v1",
        model: "gpt-saved",
        gooseBaseUrl: "",
        hasGooseSecret: false,
        configured: true,
      },
    });
    mocks.discoverModels.mockResolvedValue({
      models: [{ id: "gpt-saved", displayName: "GPT Saved" }],
    });
    mocks.updateLLMConfig.mockResolvedValue({
      config: {
        harness: AgentHarness.NATIVE,
        provider: LLMProvider.LLM_PROVIDER_OPENAI,
        hasApiKey: false,
        baseUrl: "https://api.openai.com/v1",
        model: "gpt-saved",
        gooseBaseUrl: "",
        hasGooseSecret: false,
        configured: false,
      },
    });

    render(<LLMProviderSettings />);

    const removeButton = await screen.findByRole("button", { name: "Remove key" });
    await waitFor(() => expect(screen.getByRole("button", { name: "Model" })).toHaveTextContent("GPT Saved"));
    fireEvent.click(removeButton);

    const saveButton = screen.getByRole("button", { name: "Save Minerbot settings" });
    expect(saveButton).toBeEnabled();
    fireEvent.click(saveButton);

    await waitFor(() =>
      expect(mocks.updateLLMConfig).toHaveBeenCalledWith(
        expect.objectContaining({
          apiKey: "",
          clearApiKey: true,
          model: "gpt-saved",
        }),
      ),
    );
  });
});
