import { beforeEach, expect, test } from "vitest";

import { useChatStore } from "./useChatStore";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

beforeEach(() => useChatStore.getState().resetSession());

test("archives only actual messages and restores their order after tool activity", () => {
  const chat = useChatStore.getState();
  expect(chat.history).toEqual([]);
  const id = chat.conversationId;
  chat.addMessage("user", "First actual question");
  chat.beginToolActivity("call", "Read fleet");
  chat.finishToolActivity("call", true, "Read completed");
  chat.addMessage("assistant", "First actual answer");
  chat.startNewConversation();
  chat.addMessage("user", "Second actual question");
  chat.loadConversation(id);
  chat.addMessage("user", "Follow-up");
  expect(useChatStore.getState().messages.map(({ content, sequence }) => ({ content, sequence }))).toEqual([
    { content: "First actual question", sequence: 0 },
    { content: "First actual answer", sequence: 1 },
    { content: "Follow-up", sequence: 2 },
  ]);
  expect(useChatStore.getState().history.map((thread) => thread.title)).toEqual([
    "Second actual question",
    "First actual question",
  ]);
});

test("logout removes archived history and prevents loading a prior user's conversation", () => {
  const chat = useChatStore.getState();
  const privateId = chat.conversationId;
  chat.addMessage("user", "Private question");
  chat.addMessage("assistant", "Private fleet details");
  chat.startNewConversation();
  expect(useChatStore.getState().history).toHaveLength(1);
  useFleetStore.getState().auth.logout();
  chat.loadConversation(privateId);
  expect(useChatStore.getState().history).toEqual([]);
  expect(useChatStore.getState().messages).toEqual([]);
  expect(useChatStore.getState().conversationId).not.toBe(privateId);
});
