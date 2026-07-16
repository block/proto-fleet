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
  status: "running" | "completed" | "failed";
  timestamp: Date;
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
