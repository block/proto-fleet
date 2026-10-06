import { create } from "zustand";
import { immer } from "zustand/middleware/immer";

import type { AgentActivity, ChatHistoryThread, ChatMessage, ChatSuggestion, ToolConfirmation } from "./types";

interface ChatState {
  isOpen: boolean;
  sessionVersion: number;
  conversationId: string;
  history: ChatHistoryThread[];
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
  startNewConversation: () => void;
  loadConversation: (id: string) => void;
  resetSession: () => void;
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

const emptyConversation = () => ({
  conversationId: crypto.randomUUID(),
  messages: [] as ChatMessage[],
  agentActivities: [] as AgentActivity[],
  toolConfirmations: [] as ToolConfirmation[],
  isStreaming: false,
  streamingContent: "",
  streamError: "",
  nextSequence: 0,
});

function archiveConversation(state: ChatState) {
  if (!state.messages.length) return;
  const title = state.messages.find((message) => message.role === "user")?.content ?? "Conversation";
  state.history = [
    {
      id: state.conversationId,
      title: Array.from(title).slice(0, 80).join(""),
      messages: state.messages.map((message) => ({ ...message })),
      updatedAt: state.messages[state.messages.length - 1].timestamp,
    },
    ...state.history.filter((thread) => thread.id !== state.conversationId),
  ];
}

export const useChatStore = create<ChatState>()(
  immer((set) => ({
    ...emptyConversation(),
    isOpen: false,
    sessionVersion: 0,
    history: [],
    suggestions: DEFAULT_SUGGESTIONS,

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
    startNewConversation: () =>
      set((state) => {
        archiveConversation(state);
        Object.assign(state, emptyConversation());
      }),
    loadConversation: (id) =>
      set((state) => {
        const thread = state.history.find((candidate) => candidate.id === id);
        if (!thread || id === state.conversationId) return;
        archiveConversation(state);
        Object.assign(state, emptyConversation(), {
          conversationId: thread.id,
          messages: thread.messages.map((message, sequence) => ({ ...message, sequence })),
          nextSequence: thread.messages.length,
        });
      }),
    resetSession: () =>
      set((state) => {
        Object.assign(state, emptyConversation(), {
          isOpen: false,
          history: [],
          sessionVersion: state.sessionVersion + 1,
        });
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
