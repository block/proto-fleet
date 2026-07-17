import { AnimatePresence, motion } from "motion/react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { Link } from "react-router-dom";

import AgentActivityStatus from "./AgentActivityStatus";
import ChatHeaderButton from "./ChatHeaderButton";
import ChatInput from "./ChatInput";
import ChatMessageContent from "./ChatMessageContent";
import ToolConfirmationCard from "./ToolConfirmationCard";
import type { ToolConfirmation } from "./types";
import { useChatStore } from "./useChatStore";
import { chatClient } from "@/protoFleet/api/clients";
import { ChatRole, ToolConfirmationDecision } from "@/protoFleet/api/generated/chat/v1/chat_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

const ChatPanel = () => {
  const isOpen = useChatStore((state) => state.isOpen);
  const messages = useChatStore((state) => state.messages);
  const agentActivities = useChatStore((state) => state.agentActivities);
  const toolConfirmations = useChatStore((state) => state.toolConfirmations);
  const isStreaming = useChatStore((state) => state.isStreaming);
  const streamingContent = useChatStore((state) => state.streamingContent);
  const streamError = useChatStore((state) => state.streamError);
  const suggestions = useChatStore((state) => state.suggestions);
  const close = useChatStore((state) => state.close);
  const addMessage = useChatStore((state) => state.addMessage);
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
  const conversationEndRef = useRef<HTMLDivElement>(null);
  const conversationIdRef = useRef(crypto.randomUUID());
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

  const handleConfirmation = useCallback(
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
            getErrorMessage(error, "Proto AI could not submit this confirmation. Try again."),
          );
        }
      }
    },
    [failToolConfirmation, resolveToolConfirmation, submitToolConfirmation],
  );

  const handleSend = useCallback(
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
          setStreamError(getErrorMessage(error, "Proto AI could not complete this request."));
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

  const handleNewChat = useCallback(() => {
    requestGenerationRef.current += 1;
    activeRequestRef.current?.abort();
    activeRequestRef.current = null;
    setStreaming(false);
    conversationIdRef.current = crypto.randomUUID();
    clearMessages();
  }, [clearMessages, setStreaming]);

  useEffect(
    () => () => {
      requestGenerationRef.current += 1;
      activeRequestRef.current?.abort();
      activeRequestRef.current = null;
      setStreaming(false);
    },
    [setStreaming],
  );

  useEffect(() => {
    if (!isOpen) return;
    conversationEndRef.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
  }, [agentActivities, isOpen, isStreaming, messages, toolConfirmations]);

  useEffect(() => {
    if (!isOpen) return;
    const handleEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("keydown", handleEscape);
    return () => window.removeEventListener("keydown", handleEscape);
  }, [close, isOpen]);

  return (
    <AnimatePresence>
      {isOpen ? (
        <motion.section
          id="ai-chat-panel"
          aria-label="Proto AI chat"
          className="fixed right-6 bottom-6 z-[45] flex max-h-[min(760px,calc(100dvh-48px))] w-[440px] flex-col overflow-hidden rounded-3xl border border-border-5 bg-surface-elevated-base text-text-primary shadow-300 phone:right-0 phone:bottom-0 phone:left-0 phone:max-h-[calc(100dvh-12px)] phone:w-auto phone:rounded-b-none"
          data-testid="ai-chat-panel"
          initial={{ opacity: 0, y: 32, scale: 0.98 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: 32, scale: 0.98 }}
          transition={{ duration: 0.25, ease: [0.47, 0, 0.23, 1] }}
        >
          <header className="flex shrink-0 items-center justify-between border-b border-border-5 px-5 py-4">
            <ChatHeaderButton onClose={close} />
            {hasConversation ? (
              <button
                type="button"
                className="rounded-lg px-2 py-1 text-200 text-text-primary-50 outline-none hover:bg-core-primary-5 hover:text-text-primary focus-visible:ring-2 focus-visible:ring-core-primary-20"
                onClick={handleNewChat}
              >
                New chat
              </button>
            ) : (
              <span className="rounded-full bg-intent-warning-10 px-2 py-1 text-emphasis-200 text-text-warning">
                Beta
              </span>
            )}
          </header>

          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pt-5 pb-4">
            {hasConversation ? (
              <div aria-label="Conversation" aria-live="polite" className="flex flex-col gap-4">
                {conversationItems.map((item) =>
                  item.kind === "activity" ? (
                    <AgentActivityStatus key={`activity-${item.activity.id}`} activity={item.activity} />
                  ) : item.kind === "confirmation" ? (
                    <ToolConfirmationCard
                      key={`confirmation-${item.confirmation.id}`}
                      confirmation={item.confirmation}
                      onResolve={handleConfirmation}
                    />
                  ) : (
                    <div
                      key={`message-${item.message.id}`}
                      className={`flex ${item.message.role === "user" ? "justify-end" : "justify-start"}`}
                    >
                      <div
                        className={`rounded-2xl px-4 py-3 text-300 ${
                          item.message.role === "user"
                            ? "max-w-[85%] rounded-br-md bg-core-primary-fill whitespace-pre-wrap text-text-contrast"
                            : "max-w-full rounded-bl-md bg-core-primary-5 text-text-primary"
                        }`}
                      >
                        {item.message.role === "assistant" ? (
                          <ChatMessageContent content={item.message.content} />
                        ) : (
                          item.message.content
                        )}
                      </div>
                    </div>
                  ),
                )}
                {isStreaming && streamingContent ? (
                  <div className="flex justify-start">
                    <div className="max-w-full rounded-2xl rounded-bl-md bg-core-primary-5 px-4 py-3 text-300 text-text-primary">
                      <ChatMessageContent content={streamingContent} />
                    </div>
                  </div>
                ) : null}
                {isStreaming && !streamingContent && !hasPendingConfirmation ? (
                  <div className="flex justify-start" aria-label="Proto AI is responding" role="status">
                    <div className="flex items-center gap-1 rounded-2xl rounded-bl-md bg-core-primary-5 px-4 py-4">
                      {[0, 1, 2].map((dot) => (
                        <span
                          key={dot}
                          className="size-1.5 animate-pulse rounded-full bg-text-primary-50"
                          style={{ animationDelay: `${dot * 120}ms` }}
                        />
                      ))}
                    </div>
                  </div>
                ) : null}
                {streamError ? (
                  <div
                    role="alert"
                    className="rounded-xl border border-intent-critical-20 bg-intent-critical-10 p-3 text-200 text-text-critical"
                  >
                    <p>{streamError}</p>
                    <Link
                      className="mt-2 inline-block text-emphasis-200 underline underline-offset-2"
                      to="/settings/agents"
                      onClick={close}
                    >
                      Open AI settings
                    </Link>
                  </div>
                ) : null}
                <div ref={conversationEndRef} />
              </div>
            ) : (
              <div className="flex flex-col gap-6">
                <div>
                  <h2 className="text-heading-200 text-text-primary">What would you like to know?</h2>
                  <p className="mt-1 text-300 text-text-primary-50">
                    Ask about fleet health, miner status, sites, or mining pools.
                  </p>
                </div>

                <div aria-label="Suggested prompts" className="flex flex-wrap gap-2">
                  {suggestions.map((suggestion) => (
                    <button
                      key={suggestion.label}
                      type="button"
                      className="flex items-center gap-1.5 rounded-full border border-border-10 bg-surface-base px-3 py-2 text-left text-200 text-text-primary outline-none hover:bg-core-primary-5 focus-visible:ring-2 focus-visible:ring-core-primary-20"
                      onClick={() => handleSend(suggestion.label)}
                    >
                      {suggestion.icon === "star" ? (
                        <span aria-hidden="true" className="text-text-accent">
                          ✦
                        </span>
                      ) : null}
                      {suggestion.label}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>

          <div className="shrink-0 border-t border-border-5 bg-surface-elevated-base px-5 pt-4 pb-5 phone:pb-[max(20px,env(safe-area-inset-bottom))]">
            <ChatInput disabled={isStreaming} onSend={handleSend} />
            <p className="mt-2 text-center text-200 text-text-primary-30">
              Proto AI asks for confirmation before making changes.
            </p>
          </div>
        </motion.section>
      ) : null}
    </AnimatePresence>
  );
};

export default ChatPanel;
