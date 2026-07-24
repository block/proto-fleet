import { type ReactNode, type Ref } from "react";
import clsx from "clsx";

import AgentActivityStatus from "./AgentActivityStatus";
import ChatMessageContent from "./ChatMessageContent";
import ToolConfirmationCard from "./ToolConfirmationCard";
import type { ToolConfirmation } from "./types";
import type { MinerbotConversationItem } from "./useMinerbotConversation";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";

type ChatConversationProps = {
  conversationEndRef?: Ref<HTMLDivElement>;
  conversationItems: MinerbotConversationItem[];
  header?: ReactNode;
  hasPendingConfirmation: boolean;
  isStreaming: boolean;
  layout?: "panel" | "fullScreen";
  onOpenSettings: () => void;
  onResolveConfirmation: (confirmation: ToolConfirmation, decision: "approve" | "cancel") => void;
  streamError: string;
  streamingContent: string;
};

const ChatConversation = ({
  conversationEndRef,
  conversationItems,
  header,
  hasPendingConfirmation,
  isStreaming,
  layout = "panel",
  onOpenSettings,
  onResolveConfirmation,
  streamError,
  streamingContent,
}: ChatConversationProps) => {
  const isFullScreen = layout === "fullScreen";

  return (
    <div
      aria-label="Conversation"
      aria-live="polite"
      className={clsx("flex flex-col", isFullScreen ? "mx-auto w-full max-w-[800px] gap-6" : "gap-4")}
    >
      {header ? <div className="border-b border-border-5 pb-5">{header}</div> : null}
      {conversationItems.map((item) =>
        item.kind === "activity" ? (
          <AgentActivityStatus key={`activity-${item.activity.id}`} activity={item.activity} />
        ) : item.kind === "confirmation" ? (
          <ToolConfirmationCard
            key={`confirmation-${item.confirmation.id}`}
            confirmation={item.confirmation}
            onResolve={onResolveConfirmation}
          />
        ) : (
          <div
            key={`message-${item.message.id}`}
            className={`flex ${item.message.role === "user" ? "justify-end" : "justify-start"}`}
          >
            <div
              className={clsx(
                "text-300",
                item.message.role === "user"
                  ? "max-w-[85%] rounded-2xl rounded-br-md bg-core-primary-fill px-4 py-3 whitespace-pre-wrap text-text-contrast"
                  : isFullScreen
                    ? "w-full text-text-primary"
                    : "max-w-full rounded-2xl rounded-bl-md bg-core-primary-5 px-4 py-3 text-text-primary",
              )}
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
          <div
            className={clsx(
              "text-300 text-text-primary",
              isFullScreen ? "w-full" : "max-w-full rounded-2xl rounded-bl-md bg-core-primary-5 px-4 py-3",
            )}
          >
            <ChatMessageContent content={streamingContent} />
          </div>
        </div>
      ) : null}
      {isStreaming && !streamingContent && !hasPendingConfirmation ? (
        <div className="flex justify-start" aria-label="Minerbot is responding" role="status">
          <div
            className={clsx(
              "flex items-center gap-1",
              isFullScreen ? "py-2" : "rounded-2xl rounded-bl-md bg-core-primary-5 px-4 py-4",
            )}
          >
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
        <Callout
          buttonOnClick={onOpenSettings}
          buttonText="Open Minerbot settings"
          intent="danger"
          prefixIcon={<Alert />}
          title={streamError}
        />
      ) : null}
      <div ref={conversationEndRef} />
    </div>
  );
};

export default ChatConversation;
