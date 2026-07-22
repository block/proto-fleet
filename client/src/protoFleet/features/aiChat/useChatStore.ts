import { create } from "zustand";
import { immer } from "zustand/middleware/immer";

import type { AgentActivity, ChatMessage, ChatSuggestion, ChatTranscriptTurn, ToolConfirmation } from "./types";

interface ChatState {
  isOpen: boolean;
  messages: ChatMessage[];
  agentActivities: AgentActivity[];
  toolConfirmations: ToolConfirmation[];
  isStreaming: boolean;
  streamingContent: string;
  streamError: string;
  suggestions: ChatSuggestion[];
  nextSequence: number;

  toggle: () => void;
  open: () => void;
  close: () => void;
  addMessage: (role: ChatMessage["role"], content: string) => void;
  loadMessages: (turns: ChatTranscriptTurn[]) => void;
  setStreaming: (streaming: boolean) => void;
  appendStreamingContent: (content: string) => void;
  setStreamError: (error: string) => void;
  beginToolActivity: (id: string, summary: string) => void;
  finishToolActivity: (id: string, success: boolean, summary: string, cancelled?: boolean) => void;
  addToolConfirmation: (confirmation: Omit<ToolConfirmation, "status" | "sequence">) => void;
  submitToolConfirmation: (id: string, decision: "approve" | "cancel") => void;
  resolveToolConfirmation: (id: string, decision: "approve" | "cancel") => void;
  failToolConfirmation: (id: string, error: string) => void;
  expirePendingConfirmations: () => void;
  resetStream: () => void;
  clearMessages: () => void;
}

const DEFAULT_SUGGESTIONS: ChatSuggestion[] = [
  { label: "Summarize fleet health", icon: "star" },
  { label: "How many miners are offline?" },
  { label: "Compare miner states by site" },
  { label: "List my sites" },
  { label: "Show configured mining pools" },
];

export const useChatStore = create<ChatState>()(
  immer((set) => ({
    isOpen: false,
    messages: [],
    agentActivities: [],
    toolConfirmations: [],
    isStreaming: false,
    streamingContent: "",
    streamError: "",
    suggestions: DEFAULT_SUGGESTIONS,
    nextSequence: 0,

    toggle: () =>
      set((state) => {
        state.isOpen = !state.isOpen;
      }),
    open: () =>
      set((state) => {
        state.isOpen = true;
      }),
    close: () =>
      set((state) => {
        state.isOpen = false;
      }),

    addMessage: (role, content) =>
      set((state) => {
        state.messages.push({
          id: crypto.randomUUID(),
          role,
          content,
          timestamp: new Date(),
          sequence: state.nextSequence,
        });
        state.nextSequence += 1;
      }),
    loadMessages: (turns) =>
      set((state) => {
        state.messages = turns.map((turn, index) => ({
          id: crypto.randomUUID(),
          role: turn.role,
          content: turn.content,
          timestamp: new Date(),
          sequence: index,
        }));
        state.agentActivities = [];
        state.toolConfirmations = [];
        state.streamingContent = "";
        state.streamError = "";
        state.nextSequence = turns.length;
      }),

    setStreaming: (streaming) =>
      set((state) => {
        state.isStreaming = streaming;
      }),
    appendStreamingContent: (content) =>
      set((state) => {
        state.streamingContent += content;
      }),
    setStreamError: (error) =>
      set((state) => {
        state.streamError = error;
      }),
    beginToolActivity: (id, summary) =>
      set((state) => {
        state.agentActivities.push({
          id,
          summary,
          status: "running",
          timestamp: new Date(),
          sequence: state.nextSequence,
        });
        state.nextSequence += 1;
      }),
    finishToolActivity: (id, success, summary, cancelled = false) =>
      set((state) => {
        const activity = state.agentActivities.find((candidate) => candidate.id === id);
        if (!activity) return;
        activity.summary = summary;
        activity.status = cancelled ? "cancelled" : success ? "completed" : "failed";
        const confirmation = state.toolConfirmations.find((candidate) => candidate.toolCallId === id);
        if (confirmation?.decision) {
          confirmation.status = confirmation.decision === "approve" ? "approved" : "cancelled";
          confirmation.error = undefined;
        }
      }),
    addToolConfirmation: (confirmation) =>
      set((state) => {
        state.toolConfirmations.push({
          ...confirmation,
          status: "pending",
          sequence: state.nextSequence,
        });
        state.nextSequence += 1;
      }),
    submitToolConfirmation: (id, decision) =>
      set((state) => {
        const confirmation = state.toolConfirmations.find((candidate) => candidate.id === id);
        if (!confirmation || confirmation.status !== "pending") return;
        confirmation.status = "submitting";
        confirmation.decision = decision;
        confirmation.error = undefined;
      }),
    resolveToolConfirmation: (id, decision) =>
      set((state) => {
        const confirmation = state.toolConfirmations.find((candidate) => candidate.id === id);
        if (!confirmation) return;
        confirmation.status = decision === "approve" ? "approved" : "cancelled";
        confirmation.decision = decision;
        confirmation.error = undefined;
      }),
    failToolConfirmation: (id, error) =>
      set((state) => {
        const confirmation = state.toolConfirmations.find((candidate) => candidate.id === id);
        if (!confirmation) return;
        confirmation.status = "pending";
        confirmation.error = error;
      }),
    expirePendingConfirmations: () =>
      set((state) => {
        state.toolConfirmations.forEach((confirmation) => {
          if (confirmation.status === "pending" || confirmation.status === "submitting") {
            confirmation.status = "expired";
            confirmation.error = undefined;
          }
        });
      }),
    resetStream: () =>
      set((state) => {
        state.streamingContent = "";
        state.streamError = "";
      }),
    clearMessages: () =>
      set((state) => {
        state.messages = [];
        state.agentActivities = [];
        state.toolConfirmations = [];
        state.streamingContent = "";
        state.streamError = "";
        state.nextSequence = 0;
      }),
  })),
);
