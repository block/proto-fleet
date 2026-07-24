import { AnimatePresence, motion } from "motion/react";

import { useChatStore } from "./useChatStore";
import { AI } from "@/shared/assets/icons";

const ChatFab = () => {
  const isOpen = useChatStore((state) => state.isOpen);
  const open = useChatStore((state) => state.open);

  return (
    <AnimatePresence>
      {!isOpen ? (
        <motion.button
          type="button"
          aria-controls="ai-chat-panel"
          aria-expanded={false}
          aria-label="Open Minerbot"
          className="fixed right-6 bottom-6 z-[45] flex size-14 items-center justify-center rounded-full bg-core-accent-fill text-text-base-contrast-static shadow-300 outline-none hover:opacity-80 focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base phone:right-4 phone:bottom-4"
          data-testid="ai-chat-fab"
          initial={{ opacity: 0, scale: 0.8 }}
          animate={{ opacity: 1, scale: 1 }}
          exit={{ opacity: 0, scale: 0.8 }}
          transition={{ duration: 0.2 }}
          whileTap={{ scale: 0.94 }}
          onClick={open}
        >
          <AI innerShadow={false} width="w-6" />
        </motion.button>
      ) : null}
    </AnimatePresence>
  );
};

export default ChatFab;
