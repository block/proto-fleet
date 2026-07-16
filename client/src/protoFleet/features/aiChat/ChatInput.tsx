import { FormEvent, KeyboardEvent, useState } from "react";

import { ArrowUp } from "@/shared/assets/icons";

type ChatInputProps = {
  disabled?: boolean;
  onSend: (content: string) => void;
};

const ChatInput = ({ disabled = false, onSend }: ChatInputProps) => {
  const [content, setContent] = useState("");
  const canSend = content.trim().length > 0 && !disabled;

  const submit = () => {
    const nextMessage = content.trim();
    if (!nextMessage || disabled) return;
    onSend(nextMessage);
    setContent("");
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    submit();
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submit();
    }
  };

  return (
    <form className="rounded-2xl border border-border-10 bg-surface-base p-2 shadow-100" onSubmit={handleSubmit}>
      <label className="sr-only" htmlFor="ai-chat-input">
        Message Proto AI
      </label>
      <div className="flex items-end gap-2">
        <textarea
          id="ai-chat-input"
          aria-label="Message Proto AI"
          className="max-h-24 min-h-10 min-w-0 flex-1 resize-none bg-transparent px-2 py-2 text-300 text-text-primary outline-none placeholder:text-text-primary-50 pointer-coarse:text-400"
          disabled={disabled}
          onChange={(event) => setContent(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Ask about your fleet…"
          rows={1}
          value={content}
        />
        <button
          type="submit"
          aria-label="Send message"
          className="flex size-10 shrink-0 items-center justify-center rounded-full bg-core-primary-fill text-text-contrast outline-none hover:opacity-80 focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base disabled:cursor-not-allowed disabled:opacity-30"
          disabled={!canSend}
        >
          <ArrowUp width="w-5" />
        </button>
      </div>
    </form>
  );
};

export default ChatInput;
