import { Dismiss, LogoAlt } from "@/shared/assets/icons";

type ChatHeaderButtonProps = {
  onClose: () => void;
};

const ChatHeaderButton = ({ onClose }: ChatHeaderButtonProps) => (
  <button
    type="button"
    aria-label="Close AI chat"
    className="flex items-center gap-2 rounded-full bg-core-primary-5 py-1.5 pr-2 pl-3 text-emphasis-300 text-text-primary outline-none hover:bg-core-primary-10 focus-visible:ring-2 focus-visible:ring-core-primary-20"
    onClick={onClose}
  >
    <LogoAlt width="w-4" />
    <span>Proto AI</span>
    <Dismiss width="w-3.5" />
  </button>
);

export default ChatHeaderButton;
