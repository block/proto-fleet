import { FormEvent, KeyboardEvent, useState } from "react";

import { ArrowUp } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";

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
        Message Minerbot
      </label>
      <div className="flex items-end gap-2">
        <textarea
          id="ai-chat-input"
          aria-label="Message Minerbot"
          className="max-h-24 min-h-10 min-w-0 flex-1 resize-none bg-transparent px-2 py-2 text-300 text-text-primary outline-none placeholder:text-text-primary-50 pointer-coarse:text-400"
          disabled={disabled}
          onChange={(event) => setContent(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Ask about your fleet…"
          rows={1}
          value={content}
        />
        <Button
          ariaLabel="Send message"
          className="size-10 shrink-0 !p-0"
          disabled={!canSend}
          onClick={submit}
          prefixIcon={<ArrowUp width="w-5" />}
          variant={variants.primary}
        />
      </div>
    </form>
  );
};

export default ChatInput;
