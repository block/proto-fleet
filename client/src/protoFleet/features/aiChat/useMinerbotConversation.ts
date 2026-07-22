import { useCallback, useEffect, useMemo, useRef } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { getChatContext } from "./chatContext";
import type { AgentActivity, ChatMessage, ChatTranscriptTurn, ToolConfirmation } from "./types";
import { useChatStore } from "./useChatStore";
import { chatClient } from "@/protoFleet/api/clients";
import { ChatRole, ToolConfirmationDecision } from "@/protoFleet/api/generated/chat/v1/chat_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

export type MinerbotConversationItem =
  | { kind: "message"; sequence: number; message: ChatMessage }
  | { kind: "activity"; sequence: number; activity: AgentActivity }
  | { kind: "confirmation"; sequence: number; confirmation: ToolConfirmation };

const useMinerbotConversation = () => {
  const messages = useChatStore((state) => state.messages);
  const agentActivities = useChatStore((state) => state.agentActivities);
  const toolConfirmations = useChatStore((state) => state.toolConfirmations);
  const isStreaming = useChatStore((state) => state.isStreaming);
  const streamingContent = useChatStore((state) => state.streamingContent);
  const streamError = useChatStore((state) => state.streamError);
  const addMessage = useChatStore((state) => state.addMessage);
  const loadMessages = useChatStore((state) => state.loadMessages);
  const setStreaming = useChatStore((state) => state.setStreaming);
  const appendStreamingContent = useChatStore((state) => state.appendStreamingContent);
  const setStreamError = useChatStore((state) => state.setStreamError);
  const beginToolActivity = useChatStore((state) => state.beginToolActivity);
  const finishToolActivity = useChatStore((state) => state.finishToolActivity);
  const addToolConfirmation = useChatStore((state) => state.addToolConfirmation);
  const submitToolConfirmation = useChatStore((state) => state.submitToolConfirmation);
  const resolveToolConfirmation = useChatStore((state) => state.resolveToolConfirmation);
  const failToolConfirmation = useChatStore((state) => state.failToolConfirmation);
  const expirePendingConfirmations = useChatStore((state) => state.expirePendingConfirmations);
  const resetStream = useChatStore((state) => state.resetStream);
  const clearMessages = useChatStore((state) => state.clearMessages);
  const location = useLocation();
  const navigate = useNavigate();
  const chatContext = useMemo(() => getChatContext(location.pathname), [location.pathname]);
  const conversationIdRef = useRef<string>(crypto.randomUUID());
  const requestGenerationRef = useRef(0);
  const activeRequestRef = useRef<AbortController | null>(null);
  const hasConversation = messages.length > 0;
  const hasPendingConfirmation = toolConfirmations.some(
    (confirmation) => confirmation.status === "pending" || confirmation.status === "submitting",
  );
  const conversationItems = useMemo(
    () =>
      [
        ...messages.map((message) => ({ kind: "message" as const, sequence: message.sequence, message })),
        ...agentActivities.map((activity) => ({ kind: "activity" as const, sequence: activity.sequence, activity })),
        ...toolConfirmations.map((confirmation) => ({
          kind: "confirmation" as const,
          sequence: confirmation.sequence,
          confirmation,
        })),
      ].sort((first, second) => first.sequence - second.sequence),
    [agentActivities, messages, toolConfirmations],
  );

  const resolveConfirmation = useCallback(
    async (confirmation: ToolConfirmation, decision: "approve" | "cancel") => {
      const requestGeneration = requestGenerationRef.current;
      submitToolConfirmation(confirmation.id, decision);
      try {
        await chatClient.resolveToolConfirmation({
          confirmationId: confirmation.id,
          decision: decision === "approve" ? ToolConfirmationDecision.APPROVE : ToolConfirmationDecision.CANCEL,
        });
        if (requestGeneration === requestGenerationRef.current) {
          resolveToolConfirmation(confirmation.id, decision);
        }
      } catch (error) {
        if (requestGeneration === requestGenerationRef.current) {
          failToolConfirmation(
            confirmation.id,
            getErrorMessage(error, "Minerbot could not submit this confirmation. Try again."),
          );
        }
      }
    },
    [failToolConfirmation, resolveToolConfirmation, submitToolConfirmation],
  );

  const sendMessage = useCallback(
    async (content: string) => {
      activeRequestRef.current?.abort();
      const requestGeneration = ++requestGenerationRef.current;
      const abortController = new AbortController();
      activeRequestRef.current = abortController;
      addMessage("user", content);
      resetStream();
      setStreaming(true);

      let assistantContent = "";
      try {
        const stream = chatClient.sendMessage(
          {
            conversationId: conversationIdRef.current,
            content,
            history: messages.map((message) => ({
              role: message.role === "user" ? ChatRole.USER : ChatRole.ASSISTANT,
              content: message.content,
            })),
          },
          { signal: abortController.signal },
        );
        for await (const response of stream) {
          if (requestGeneration !== requestGenerationRef.current) break;
          switch (response.event.case) {
            case "textDelta": {
              const delta = response.event.value.content;
              assistantContent += delta;
              appendStreamingContent(delta);
              break;
            }
            case "toolCall":
              beginToolActivity(response.event.value.id, response.event.value.summary);
              break;
            case "toolResult":
              finishToolActivity(
                response.event.value.id,
                response.event.value.success,
                response.event.value.summary,
                response.event.value.cancelled,
              );
              break;
            case "confirmationRequired":
              addToolConfirmation({
                id: response.event.value.confirmationId,
                toolCallId: response.event.value.toolCallId,
                title: response.event.value.title,
                description: response.event.value.description,
                confirmLabel: response.event.value.confirmLabel,
                details: response.event.value.details.map((detail) => ({ label: detail.label, value: detail.value })),
              });
              break;
          }
        }
        if (requestGeneration === requestGenerationRef.current && assistantContent.trim()) {
          addMessage("assistant", assistantContent);
        }
      } catch (error) {
        if (requestGeneration === requestGenerationRef.current) {
          expirePendingConfirmations();
          setStreamError(getErrorMessage(error, "Minerbot could not complete this request."));
        }
      } finally {
        if (requestGeneration === requestGenerationRef.current) {
          activeRequestRef.current = null;
          setStreaming(false);
        }
      }
    },
    [
      addMessage,
      addToolConfirmation,
      appendStreamingContent,
      beginToolActivity,
      expirePendingConfirmations,
      finishToolActivity,
      messages,
      resetStream,
      setStreamError,
      setStreaming,
    ],
  );

  const startNewChat = useCallback(() => {
    requestGenerationRef.current += 1;
    activeRequestRef.current?.abort();
    activeRequestRef.current = null;
    setStreaming(false);
    conversationIdRef.current = crypto.randomUUID();
    clearMessages();
  }, [clearMessages, setStreaming]);

  const loadConversation = useCallback(
    (conversationId: string, turns: ChatTranscriptTurn[]) => {
      requestGenerationRef.current += 1;
      activeRequestRef.current?.abort();
      activeRequestRef.current = null;
      setStreaming(false);
      conversationIdRef.current = conversationId;
      loadMessages(turns);
    },
    [loadMessages, setStreaming],
  );

  const openSettings = useCallback(() => {
    navigate("/settings/agents");
  }, [navigate]);

  useEffect(
    () => () => {
      requestGenerationRef.current += 1;
      activeRequestRef.current?.abort();
      activeRequestRef.current = null;
      setStreaming(false);
    },
    [setStreaming],
  );

  return {
    chatContext,
    conversationItems,
    hasConversation,
    hasPendingConfirmation,
    isStreaming,
    loadConversation,
    openSettings,
    resolveConfirmation,
    sendMessage,
    startNewChat,
    streamError,
    streamingContent,
  };
};

export default useMinerbotConversation;
