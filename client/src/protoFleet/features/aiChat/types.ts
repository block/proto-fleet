/** Core types for the AI Chat feature */

export type MessageRole = "user" | "assistant";

export interface ChatMessage {
  id: string;
  role: MessageRole;
  content: string;
  timestamp: Date;
  sequence: number;
}

export interface AgentActivity {
  id: string;
  summary: string;
  status: "running" | "completed" | "failed" | "cancelled";
  timestamp: Date;
  sequence: number;
}

export interface ToolConfirmationDetail {
  label: string;
  value: string;
}

export interface ToolConfirmation {
  id: string;
  toolCallId: string;
  title: string;
  description: string;
  confirmLabel: string;
  details: ToolConfirmationDetail[];
  status: "pending" | "submitting" | "approved" | "cancelled" | "expired";
  decision?: "approve" | "cancel";
  error?: string;
  sequence: number;
}

export interface ChatSuggestion {
  label: string;
  icon?: "star" | "default";
}

/**
 * Configuration for the LLM provider. This is a BYOLLM solution — operators
 * configure their own provider.
 */
export interface LLMProviderConfig {
  harness: "native" | "goose";
  provider: "openai" | "anthropic" | "ollama";
  apiKey?: string;
  baseUrl?: string;
  model?: string;
  gooseBaseUrl?: string;
}
