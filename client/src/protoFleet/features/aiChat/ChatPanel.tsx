import { AnimatePresence, motion } from "motion/react";
import { useCallback, useEffect, useRef } from "react";

import ChatConversation from "./ChatConversation";
import ChatInput from "./ChatInput";
import { useChatStore } from "./useChatStore";
import useMinerbotConversation from "./useMinerbotConversation";
import { Dismiss } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants as buttonVariants } from "@/shared/components/Button";
import Chip from "@/shared/components/Chip";
import Header from "@/shared/components/Header";

const ChatPanel = () => {
  const isOpen = useChatStore((state) => state.isOpen);
  const close = useChatStore((state) => state.close);
  const {
    chatContext,
    conversationItems,
    hasConversation,
    hasPendingConfirmation,
    isStreaming,
    openSettings,
    resolveConfirmation,
    sendMessage,
    startNewChat,
    streamError,
    streamingContent,
  } = useMinerbotConversation();
  const conversationEndRef = useRef<HTMLDivElement>(null);

  const handleOpenSettings = useCallback(() => {
    close();
    openSettings();
  }, [close, openSettings]);

  useEffect(() => {
    if (!isOpen) return;
    conversationEndRef.current?.scrollIntoView({ behavior: "smooth", block: "end", inline: "nearest" });
  }, [conversationItems, isOpen, isStreaming, streamError, streamingContent]);

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
          aria-label="Minerbot chat"
          className="fixed right-6 bottom-6 z-[45] flex max-h-[min(760px,calc(100dvh-48px))] w-[min(calc(100vw-theme(spacing.12)),480px)] flex-col overflow-hidden rounded-3xl border border-border-5 bg-surface-elevated-base text-text-primary shadow-300 phone:right-0 phone:bottom-0 phone:left-0 phone:max-h-[calc(100dvh-12px)] phone:w-auto phone:rounded-b-none"
          data-testid="ai-chat-panel"
          initial={{ opacity: 0, y: 32, scale: 0.98 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: 32, scale: 0.98 }}
          transition={{ duration: 0.25, ease: [0.47, 0, 0.23, 1] }}
        >
          <header className="shrink-0 border-b border-border-5 px-5 py-4">
            <Header
              title="Minerbot"
              titleSize="text-heading-200"
              icon={<Dismiss />}
              iconAriaLabel="Close Minerbot"
              iconOnClick={close}
              inline
              centerButton
              stackButtonsOnPhone={false}
              buttonSize={buttonSizes.compact}
              buttons={
                hasConversation
                  ? [
                      {
                        text: "New chat",
                        variant: buttonVariants.secondary,
                        onClick: startNewChat,
                      },
                    ]
                  : undefined
              }
              buttonsWrapperClassName="shrink-0"
            >
              {hasConversation ? null : (
                <div className="shrink-0">
                  <Chip>Beta</Chip>
                </div>
              )}
            </Header>
          </header>

          <div
            className="min-h-0 flex-1 scroll-pb-4 overflow-y-auto overscroll-contain px-5 pt-5 pb-4"
            data-testid="ai-chat-panel-scroll-area"
          >
            {hasConversation ? (
              <ChatConversation
                conversationEndRef={conversationEndRef}
                conversationItems={conversationItems}
                hasPendingConfirmation={hasPendingConfirmation}
                isStreaming={isStreaming}
                onOpenSettings={handleOpenSettings}
                onResolveConfirmation={resolveConfirmation}
                streamError={streamError}
                streamingContent={streamingContent}
              />
            ) : (
              <div className="flex flex-col gap-6">
                <div>
                  <h2 className="text-heading-200 text-text-primary">What would you like to know?</h2>
                  <p className="mt-1 text-300 text-text-primary-50">{chatContext.description}</p>
                </div>

                <div aria-label="Suggested prompts" className="flex flex-wrap gap-2">
                  {chatContext.suggestions.map((suggestion) => (
                    <Button
                      key={suggestion.label}
                      size={buttonSizes.compact}
                      text={suggestion.label}
                      onClick={() => sendMessage(suggestion.label)}
                      variant={buttonVariants.secondary}
                    />
                  ))}
                </div>
              </div>
            )}
          </div>

          <div className="shrink-0 border-t border-border-5 bg-surface-elevated-base px-5 pt-4 pb-5 phone:pb-[max(20px,env(safe-area-inset-bottom))]">
            <ChatInput disabled={isStreaming} onSend={sendMessage} />
            <p className="mt-2 text-center text-200 text-text-primary-30">
              Minerbot asks for confirmation before making changes.
            </p>
          </div>
        </motion.section>
      ) : null}
    </AnimatePresence>
  );
};

export default ChatPanel;
