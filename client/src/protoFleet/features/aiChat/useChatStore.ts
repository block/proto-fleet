import { create } from "zustand";
import { immer } from "zustand/middleware/immer";

import type { AgentActivity, ChatMessage, ChatSuggestion } from "./types";

interface ChatState {
  isOpen: boolean;
  messages: ChatMessage[];
  agentActivities: AgentActivity[];
  isStreaming: boolean;
  streamingContent: string;
  streamError: string;
  suggestions: ChatSuggestion[];
  nextSequence: number;

  toggle: () => void;
  open: () => void;
  close: () => void;
  addMessage: (role: ChatMessage["role"], content: string) => void;
  setStreaming: (streaming: boolean) => void;
  appendStreamingContent: (content: string) => void;
  setStreamError: (error: string) => void;
  beginToolActivity: (id: string, summary: string) => void;
  finishToolActivity: (id: string, success: boolean, summary: string) => void;
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
    finishToolActivity: (id, success, summary) =>
      set((state) => {
        const activity = state.agentActivities.find((candidate) => candidate.id === id);
        if (!activity) return;
        activity.summary = summary;
        activity.status = success ? "completed" : "failed";
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
        state.streamingContent = "";
        state.streamError = "";
        state.nextSequence = 0;
      }),
  })),
);
