import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import ChatConversation from "../ChatConversation";
import ChatInput from "../ChatInput";
import useMinerbotConversation from "../useMinerbotConversation";
import {
  type MinerbotHistoryThread,
  minerbotHistoryThreads,
  minerbotSuggestionCards,
  type MinerbotSuggestionIcon,
} from "./minerbotExperience";
import { Activity, Efficiency, Graph, type IconProps, LightningAlt, Lock, Settings } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants as buttonVariants } from "@/shared/components/Button";

const suggestionIconByKey: Record<MinerbotSuggestionIcon, (props: IconProps) => ReactNode> = {
  activity: Activity,
  energy: LightningAlt,
  firmware: Settings,
  onboarding: Graph,
  profitability: Efficiency,
  security: Lock,
};

const MinerbotPage = () => {
  const {
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
  } = useMinerbotConversation();
  const [activeHistoryId, setActiveHistoryId] = useState<string | null>(null);
  const [activeView, setActiveView] = useState<"chat" | "suggestions">(hasConversation ? "chat" : "suggestions");
  const conversationEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    conversationEndRef.current?.scrollIntoView({ behavior: "smooth", block: "end", inline: "nearest" });
  }, [conversationItems, isStreaming, streamError, streamingContent]);

  const handleStartNewChat = useCallback(() => {
    setActiveHistoryId(null);
    setActiveView("chat");
    startNewChat();
  }, [startNewChat]);

  const handleShowSuggestions = useCallback(() => {
    setActiveHistoryId(null);
    setActiveView("suggestions");
    startNewChat();
  }, [startNewChat]);

  const handleSendMessage = useCallback(
    (content: string) => {
      setActiveHistoryId(null);
      setActiveView("chat");
      sendMessage(content);
    },
    [sendMessage],
  );

  const handleSuggestionClick = useCallback(
    (prompt: string) => {
      setActiveHistoryId(null);
      setActiveView("chat");
      sendMessage(prompt);
    },
    [sendMessage],
  );

  const handleLoadHistory = useCallback(
    (thread: MinerbotHistoryThread) => {
      setActiveHistoryId(thread.id);
      setActiveView("chat");
      loadConversation(thread.id, thread.messages);
    },
    [loadConversation],
  );

  const showSuggestions = activeView === "suggestions";
  const showCurrentChat = activeView === "chat" && !activeHistoryId;
  const showEmptyChatPrompts = showCurrentChat && !hasConversation;
  const activeHistoryThread = minerbotHistoryThreads.find((thread) => thread.id === activeHistoryId);

  const navItemClassName = (active = false) =>
    clsx(
      "block w-full rounded-lg px-2 py-1.5 text-left text-emphasis-300 transition-colors",
      "hover:bg-core-primary-5 focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base focus-visible:outline-none",
      active ? "bg-core-primary-5 text-text-primary" : "text-text-primary-70",
    );

  return (
    <div className="grid h-full min-h-0 overflow-hidden text-text-primary laptop:grid-cols-[260px_minmax(0,1fr)]">
      <aside className="min-h-0 overflow-y-auto border-b border-border-5 bg-surface-base laptop:border-r laptop:border-b-0">
        <nav aria-label="Minerbot">
          <ul className="flex w-full shrink-0 flex-col gap-8 px-3 pt-6 text-text-primary-70">
            <li>
              <ul className="flex flex-col">
                <li>
                  <button
                    aria-current={showCurrentChat ? "page" : undefined}
                    className={navItemClassName(showCurrentChat)}
                    onClick={handleStartNewChat}
                    type="button"
                  >
                    New chat
                  </button>
                </li>
                <li>
                  <button
                    aria-current={showSuggestions ? "page" : undefined}
                    className={navItemClassName(showSuggestions)}
                    onClick={handleShowSuggestions}
                    type="button"
                  >
                    Suggestions
                  </button>
                </li>
              </ul>
            </li>
            <li>
              <div className="px-2 pb-2 text-300 font-medium text-text-primary-50">History</div>
              <ul aria-label="Chat history" className="flex flex-col">
                {minerbotHistoryThreads.map((item) => (
                  <li key={item.title}>
                    <button
                      aria-current={activeHistoryId === item.id ? "page" : undefined}
                      className={navItemClassName(activeHistoryId === item.id)}
                      onClick={() => handleLoadHistory(item)}
                      type="button"
                    >
                      <span className="block truncate">{item.title}</span>
                    </button>
                  </li>
                ))}
              </ul>
            </li>
          </ul>
        </nav>
      </aside>

      <section className="flex min-h-0 flex-col overflow-hidden bg-surface-elevated-base">
        <div
          className="min-h-0 flex-1 scroll-pb-8 overflow-y-auto overscroll-contain px-5 py-5 laptop:px-8 laptop:py-8"
          data-testid="minerbot-chat-scroll-area"
        >
          {showSuggestions ? (
            <div className="mx-auto flex min-h-full w-full max-w-6xl flex-col justify-center gap-6 py-8">
              <div className="max-w-2xl">
                <h1 className="text-heading-300">What should Minerbot work on?</h1>
                <p className="mt-2 text-300 text-text-primary-50">{chatContext.description}</p>
              </div>
              <div aria-label="Actionable suggestions" className="grid gap-3 tablet:grid-cols-2 desktop:grid-cols-3">
                {minerbotSuggestionCards.map((suggestion) => {
                  const SuggestionIcon = suggestionIconByKey[suggestion.icon];

                  return (
                    <article
                      key={suggestion.title}
                      className="flex min-h-48 flex-col rounded-lg border border-border-5 bg-surface-base p-4 shadow-50"
                      data-testid="minerbot-suggestion-card"
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-core-accent-10 text-text-emphasis">
                          <SuggestionIcon width="w-4" />
                        </div>
                        <span className="rounded-full bg-core-primary-5 px-2 py-1 text-200 font-medium whitespace-nowrap text-text-primary-50">
                          {suggestion.impact}
                        </span>
                      </div>

                      <h2 className="mt-4 text-heading-100">{suggestion.title}</h2>
                      <p className="mt-1 flex-1 text-300 text-text-primary-70">{suggestion.description}</p>

                      <div className="mt-4">
                        <Button
                          disabled={isStreaming}
                          onClick={() => handleSuggestionClick(suggestion.prompt)}
                          size={buttonSizes.compact}
                          text={suggestion.actionLabel}
                          variant={buttonVariants.secondary}
                        />
                      </div>
                    </article>
                  );
                })}
              </div>
            </div>
          ) : showEmptyChatPrompts ? (
            <div
              aria-label="Conversation"
              aria-live="polite"
              className="mx-auto flex min-h-full w-full max-w-[800px] flex-col justify-center gap-6 py-8"
            >
              <div>
                <h1 className="text-heading-200 text-text-primary">What would you like to know?</h1>
                <p className="mt-1 text-300 text-text-primary-50">{chatContext.description}</p>
              </div>

              <div aria-label="Suggested prompts" className="flex flex-wrap gap-2">
                {chatContext.suggestions.map((suggestion) => (
                  <Button
                    key={suggestion.label}
                    disabled={isStreaming}
                    onClick={() => handleSuggestionClick(suggestion.label)}
                    size={buttonSizes.compact}
                    text={suggestion.label}
                    variant={buttonVariants.secondary}
                  />
                ))}
              </div>
              <div ref={conversationEndRef} />
            </div>
          ) : (
            <ChatConversation
              conversationEndRef={conversationEndRef}
              conversationItems={conversationItems}
              header={
                activeHistoryThread ? (
                  <div>
                    <h1 className="text-heading-200 text-text-primary">{activeHistoryThread.title}</h1>
                    <p className="mt-1 text-300 text-text-primary-50">{activeHistoryThread.timeLabel}</p>
                  </div>
                ) : undefined
              }
              hasPendingConfirmation={hasPendingConfirmation}
              isStreaming={isStreaming}
              layout="fullScreen"
              onOpenSettings={openSettings}
              onResolveConfirmation={resolveConfirmation}
              streamError={streamError}
              streamingContent={streamingContent}
            />
          )}
        </div>

        <div className="shrink-0 border-t border-border-5 bg-surface-elevated-base px-5 pt-4 pb-5 laptop:px-8">
          <div className="mx-auto w-full max-w-[800px]">
            <ChatInput disabled={isStreaming} onSend={handleSendMessage} />
          </div>
        </div>
      </section>
    </div>
  );
};

export default MinerbotPage;
