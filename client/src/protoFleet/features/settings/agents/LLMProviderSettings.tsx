import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { chatClient } from "@/protoFleet/api/clients";
import { AgentHarness, type LLMConfig, LLMProvider } from "@/protoFleet/api/generated/chat/v1/chat_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type FormState = {
  harness: AgentHarness;
  provider: LLMProvider;
  apiKey: string;
  baseUrl: string;
  model: string;
  gooseBaseUrl: string;
  gooseSecret: string;
  hasApiKey: boolean;
  hasGooseSecret: boolean;
  clearApiKey: boolean;
  clearGooseSecret: boolean;
  storedApiKeyProvider: LLMProvider;
};

type DiscoveredModel = {
  id: string;
  displayName: string;
};

const EMPTY_FORM: FormState = {
  harness: AgentHarness.NATIVE,
  provider: LLMProvider.LLM_PROVIDER_UNSPECIFIED,
  apiKey: "",
  baseUrl: "",
  model: "",
  gooseBaseUrl: "",
  gooseSecret: "",
  hasApiKey: false,
  hasGooseSecret: false,
  clearApiKey: false,
  clearGooseSecret: false,
  storedApiKeyProvider: LLMProvider.LLM_PROVIDER_UNSPECIFIED,
};

const HARNESS_OPTIONS = [
  {
    value: String(AgentHarness.NATIVE),
    label: "Embedded agent",
    description: "Runs the provider and read-only fleet tool loop inside Fleet.",
  },
  {
    value: String(AgentHarness.GOOSE),
    label: "Goose ACP",
    description: "Saves remote ACP settings; the server adapter is not enabled yet.",
  },
];

const PROVIDER_OPTIONS = [
  { value: String(LLMProvider.LLM_PROVIDER_OPENAI), label: "OpenAI" },
  { value: String(LLMProvider.LLM_PROVIDER_ANTHROPIC), label: "Anthropic" },
  { value: String(LLMProvider.LLM_PROVIDER_OLLAMA), label: "Ollama" },
];

const DEFAULT_BASE_URLS: Partial<Record<LLMProvider, string>> = {
  [LLMProvider.LLM_PROVIDER_OPENAI]: "https://api.openai.com/v1",
  [LLMProvider.LLM_PROVIDER_ANTHROPIC]: "https://api.anthropic.com",
  [LLMProvider.LLM_PROVIDER_OLLAMA]: "http://127.0.0.1:11434",
};

const isSupportedProvider = (provider: LLMProvider) =>
  provider === LLMProvider.LLM_PROVIDER_OPENAI ||
  provider === LLMProvider.LLM_PROVIDER_ANTHROPIC ||
  provider === LLMProvider.LLM_PROVIDER_OLLAMA;

const formFromConfig = (config?: LLMConfig): FormState => {
  const configuredProvider = config?.provider ?? LLMProvider.LLM_PROVIDER_UNSPECIFIED;
  const provider = isSupportedProvider(configuredProvider) ? configuredProvider : LLMProvider.LLM_PROVIDER_UNSPECIFIED;
  return {
    ...EMPTY_FORM,
    harness: config?.harness || AgentHarness.NATIVE,
    provider,
    baseUrl:
      provider === configuredProvider
        ? config?.baseUrl || DEFAULT_BASE_URLS[provider] || ""
        : DEFAULT_BASE_URLS[provider] || "",
    model: provider === configuredProvider ? (config?.model ?? "") : "",
    gooseBaseUrl: config?.gooseBaseUrl ?? "",
    hasApiKey: provider === configuredProvider && (config?.hasApiKey ?? false),
    hasGooseSecret: config?.hasGooseSecret ?? false,
    storedApiKeyProvider:
      provider === configuredProvider && config?.hasApiKey ? provider : LLMProvider.LLM_PROVIDER_UNSPECIFIED,
  };
};

const modelLabel = (model: DiscoveredModel) =>
  model.displayName && model.displayName !== model.id ? `${model.displayName} (${model.id})` : model.id;

const MODEL_DISCOVERY_DEBOUNCE_MS = 300;

const LLMProviderSettings = () => {
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isDiscovering, setIsDiscovering] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [modelError, setModelError] = useState("");
  const [models, setModels] = useState<DiscoveredModel[]>([]);
  const discoveryRequestRef = useRef(0);

  const loadConfig = useCallback(async () => {
    setIsLoading(true);
    setLoadError("");
    try {
      const response = await chatClient.getLLMConfig({});
      const nextForm = formFromConfig(response.config);
      setForm(nextForm);
      setModels(nextForm.model ? [{ id: nextForm.model, displayName: nextForm.model }] : []);
    } catch (error) {
      setLoadError(getErrorMessage(error, "Could not load Minerbot settings."));
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial async server sync on settings mount
    void loadConfig();
  }, [loadConfig]);

  const hasProvider = form.provider !== LLMProvider.LLM_PROVIDER_UNSPECIFIED;
  const requiresAPIKey = hasProvider && form.provider !== LLMProvider.LLM_PROVIDER_OLLAMA;
  const isGoose = form.harness === AgentHarness.GOOSE;
  const canSave = useMemo(
    () =>
      !isLoading &&
      !isSaving &&
      hasProvider &&
      form.model.trim().length > 0 &&
      form.baseUrl.trim().length > 0 &&
      (!isGoose || form.gooseBaseUrl.trim().length > 0),
    [form.baseUrl, form.gooseBaseUrl, form.model, hasProvider, isGoose, isLoading, isSaving],
  );
  const canDiscover =
    !isLoading &&
    hasProvider &&
    form.baseUrl.trim().length > 0 &&
    (!requiresAPIKey || form.apiKey.length > 0 || (form.hasApiKey && !form.clearApiKey));
  const modelOptions = useMemo(() => models.map((model) => ({ value: model.id, label: modelLabel(model) })), [models]);

  const updateField = useCallback((field: keyof FormState, value: string | boolean | number) => {
    setForm((current) => ({ ...current, [field]: value }));
  }, []);

  const handleProviderChange = useCallback((value: string) => {
    const provider = Number(value) as LLMProvider;
    discoveryRequestRef.current += 1;
    setIsDiscovering(false);
    setModels([]);
    setModelError("");
    setForm((current) => ({
      ...current,
      provider,
      baseUrl: DEFAULT_BASE_URLS[provider] ?? "",
      apiKey: "",
      model: "",
      hasApiKey: current.storedApiKeyProvider === provider,
      clearApiKey: false,
    }));
  }, []);

  const updateProviderConnection = useCallback((field: "apiKey" | "baseUrl", value: string) => {
    discoveryRequestRef.current += 1;
    setIsDiscovering(false);
    setModels([]);
    setModelError("");
    setForm((current) => ({
      ...current,
      [field]: value,
      model: "",
      ...(field === "apiKey" && value ? { clearApiKey: false } : {}),
    }));
  }, []);

  const toggleStoredAPIKey = useCallback(() => {
    discoveryRequestRef.current += 1;
    setIsDiscovering(false);
    setModelError("");
    setForm((current) => ({
      ...current,
      apiKey: "",
      clearApiKey: !current.clearApiKey,
    }));
  }, []);

  useEffect(() => {
    if (!canDiscover) return;

    const requestID = ++discoveryRequestRef.current;
    const timeoutID = window.setTimeout(() => {
      setIsDiscovering(true);
      setModelError("");
      void chatClient
        .discoverModels({
          provider: form.provider,
          apiKey: form.apiKey,
          baseUrl: form.baseUrl,
          useStoredApiKey: requiresAPIKey && !form.apiKey && form.hasApiKey && !form.clearApiKey,
        })
        .then((response) => {
          if (discoveryRequestRef.current !== requestID) return;
          setModels(response.models);
          setForm((current) => ({
            ...current,
            model: response.models.some((model) => model.id === current.model) ? current.model : "",
          }));
          if (response.models.length === 0) {
            setModelError(
              form.provider === LLMProvider.LLM_PROVIDER_OPENAI
                ? "No OpenAI models compatible with the current agent flow are available for this API key."
                : "The provider returned no available models.",
            );
          }
        })
        .catch((error: unknown) => {
          if (discoveryRequestRef.current !== requestID) return;
          setModelError(getErrorMessage(error, "Failed to fetch models from the provider."));
        })
        .finally(() => {
          if (discoveryRequestRef.current === requestID) {
            setIsDiscovering(false);
          }
        });
    }, MODEL_DISCOVERY_DEBOUNCE_MS);

    return () => {
      window.clearTimeout(timeoutID);
      if (discoveryRequestRef.current === requestID) {
        discoveryRequestRef.current += 1;
      }
    };
  }, [canDiscover, form.apiKey, form.baseUrl, form.clearApiKey, form.hasApiKey, form.provider, requiresAPIKey]);

  const handleSave = useCallback(async () => {
    if (!canSave) return;
    setIsSaving(true);
    try {
      const response = await chatClient.updateLLMConfig({
        harness: form.harness,
        provider: form.provider,
        apiKey: form.apiKey,
        baseUrl: form.baseUrl,
        model: form.model,
        gooseBaseUrl: form.gooseBaseUrl,
        gooseSecret: form.gooseSecret,
        clearApiKey: form.clearApiKey,
        clearGooseSecret: form.clearGooseSecret,
      });
      setForm(formFromConfig(response.config));
      pushToast({ message: "Minerbot settings saved", status: STATUSES.success });
    } catch (error) {
      pushToast({
        message: getErrorMessage(error, "Could not save Minerbot settings."),
        status: STATUSES.error,
      });
    } finally {
      setIsSaving(false);
    }
  }, [canSave, form]);

  if (isLoading) {
    return <div className="text-300 text-text-primary-50">Loading Minerbot settings...</div>;
  }

  return (
    <section className="flex flex-col gap-5 rounded-xl border border-border-5 p-6" aria-labelledby="ai-settings-title">
      <div>
        <h2 id="ai-settings-title" className="text-heading-200 text-text-primary">
          Minerbot
        </h2>
        <p className="mt-1 text-300 text-text-primary-50">
          Choose the agent harness and bring your own model provider. Secrets are encrypted by the Fleet server and are
          never returned to the browser.
        </p>
      </div>

      {loadError ? (
        <div role="alert" className="rounded-lg bg-intent-critical-10 p-3 text-300 text-text-critical">
          {loadError}
        </div>
      ) : null}

      <div className="grid grid-cols-2 gap-4 tablet:grid-cols-1">
        <Select
          id="ai-agent-harness"
          label="Agent harness"
          options={HARNESS_OPTIONS}
          value={String(form.harness)}
          onChange={(value) => updateField("harness", Number(value))}
        />
        <Select
          id="ai-provider"
          label="Model provider"
          options={PROVIDER_OPTIONS}
          value={String(form.provider)}
          placeholder="Select a provider"
          onChange={handleProviderChange}
        />
      </div>

      <Input
        id="ai-base-url"
        label="Provider base URL"
        initValue={form.baseUrl}
        disabled={!hasProvider}
        onChange={(value) => updateProviderConnection("baseUrl", value)}
        required
      />

      {!hasProvider ? (
        <p className="text-200 text-text-primary-50">Select a model provider to configure its connection.</p>
      ) : requiresAPIKey ? (
        <div className="flex items-center gap-3">
          <div className="min-w-0 flex-1">
            <Input
              id="ai-api-key"
              label={form.hasApiKey && !form.clearApiKey ? "API key (stored; enter to replace)" : "API key"}
              type="password"
              initValue={form.apiKey}
              onChange={(value) => updateProviderConnection("apiKey", value)}
            />
          </div>
          {form.hasApiKey ? (
            <Button
              variant={variants.textOnly}
              size={sizes.compact}
              text={form.clearApiKey ? "Keep stored key" : "Remove key"}
              onClick={toggleStoredAPIKey}
            />
          ) : null}
        </div>
      ) : (
        <p className="text-200 text-text-primary-50">Ollama does not require an API key by default.</p>
      )}

      <Select
        id="ai-model"
        label="Model"
        options={modelOptions}
        value={form.model}
        placeholder={
          !hasProvider
            ? "Select a provider first"
            : requiresAPIKey && !form.apiKey && (!form.hasApiKey || form.clearApiKey)
              ? "Enter an API key to load models"
              : isDiscovering
                ? "Loading models..."
                : models.length === 0
                  ? "Waiting for models..."
                  : "Select a model"
        }
        emptyMessage="No models available"
        disabled={!hasProvider || isDiscovering || models.length === 0}
        onChange={(value) => updateField("model", value)}
      />

      {modelError ? (
        <div role="alert" className="rounded-lg bg-intent-critical-10 p-3 text-300 text-text-critical">
          {modelError}
        </div>
      ) : null}

      {isGoose ? (
        <div className="flex flex-col gap-4 rounded-xl bg-intent-warning-10 p-4">
          <p className="text-300 text-text-warning">
            Goose ACP settings can be saved now, but chat remains unavailable until the server-side ACP adapter is
            enabled.
          </p>
          <Input
            id="goose-base-url"
            label="Goose ACP base URL"
            initValue={form.gooseBaseUrl}
            onChange={(value) => updateField("gooseBaseUrl", value)}
            required
          />
          <div className="flex items-center gap-3">
            <div className="min-w-0 flex-1">
              <Input
                id="goose-secret"
                label={
                  form.hasGooseSecret && !form.clearGooseSecret
                    ? "Goose secret (stored; enter to replace)"
                    : "Goose secret"
                }
                type="password"
                initValue={form.gooseSecret}
                onChange={(value) => {
                  updateField("gooseSecret", value);
                  if (value) updateField("clearGooseSecret", false);
                }}
              />
            </div>
            {form.hasGooseSecret ? (
              <Button
                variant={variants.textOnly}
                size={sizes.compact}
                text={form.clearGooseSecret ? "Keep stored secret" : "Remove secret"}
                onClick={() => updateField("clearGooseSecret", !form.clearGooseSecret)}
              />
            ) : null}
          </div>
        </div>
      ) : null}

      <div className="flex justify-end">
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Save Minerbot settings"
          disabled={!canSave}
          loading={isSaving}
          onClick={() => void handleSave()}
        />
      </div>
    </section>
  );
};

export default LLMProviderSettings;
