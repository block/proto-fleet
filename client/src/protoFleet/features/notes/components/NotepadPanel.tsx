import { AnimatePresence, motion } from "motion/react";
import { type ReactElement, useMemo } from "react";

import NoteCard from "./NoteCard";
import NoteComposer from "./NoteComposer";
import { useNotesFeed } from "@/protoFleet/api/useNotesFeed";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { groupNotesByDay } from "@/protoFleet/features/notes/noteFormat";
import { useHasPermission, useIsNotepadOpen, useSetNotepadOpen, useUsername } from "@/protoFleet/store";
import { Alert, Dismiss, Edit } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import { usePoll } from "@/shared/hooks/usePoll";

const PAGE_SIZE = 25;

// NotepadPanel is the org-wide shared notepad, summonable from any
// view via the PageHeader toggle. It is deliberately non-modal: no
// backdrop and no scroll lock, so the page underneath stays readable
// and interactive while the panel is open (the point of a notepad you
// consult mid-task). Toasts (z-60) stay above it.
const NotepadPanel = (): ReactElement => {
  const isOpen = useIsNotepadOpen();
  const setNotepadOpen = useSetNotepadOpen();
  const canRead = useHasPermission("note:read");
  const canCreate = useHasPermission("note:create");
  const canModerate = useHasPermission("note:manage");
  const username = useUsername();

  const { notes, isLoading, hasLoaded, error, hasMore, loadMore, refresh, refreshHead } = useNotesFeed({
    pageSize: PAGE_SIZE,
  });

  // The store flag can outlive the permission (e.g. re-login as a
  // leaner role), so visibility gates on both.
  const isVisible = isOpen && canRead;

  const close = () => setNotepadOpen(false);

  // Stack-based: a confirm Dialog opened above the panel consumes
  // Escape first; the next press closes the panel.
  useEscapeDismiss(isVisible ? close : undefined);

  // Fetch on open, then keep the head of the feed live while the
  // panel stays open. refreshHead merges into the accumulated list,
  // so a poll tick never collapses pages loaded via Load more.
  usePoll({
    fetchData: refreshHead,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    enabled: isVisible,
  });

  const showInitialSpinner = !hasLoaded && error === null;
  const dayGroups = useMemo(() => groupNotesByDay(notes), [notes]);

  return (
    <AnimatePresence>
      {isVisible ? (
        <motion.aside
          initial={{ x: "100%" }}
          animate={{ x: 0, transition: { duration: 0.25, ease: "easeOut" } }}
          exit={{ x: "100%", transition: { duration: 0.2, ease: "easeIn" } }}
          className="fixed top-0 right-0 bottom-0 z-50 flex w-full flex-col border-l border-border-10 bg-surface-elevated-base shadow-xl tablet:w-[400px]"
          role="complementary"
          aria-label="Team notepad"
          data-testid="notepad-panel"
        >
          <div className="flex items-center justify-between border-b border-border-5 px-4 py-3">
            <div className="min-w-0">
              <h2 className="text-heading-200 text-text-primary">Notepad</h2>
              <p className="text-200 text-text-primary-50">Shared with everyone on your team</p>
            </div>
            <button
              type="button"
              aria-label="Close notepad"
              data-testid="notepad-close"
              className="shrink-0 rounded-md p-1.5 text-text-primary-70 hover:cursor-pointer hover:bg-core-primary-5 hover:text-text-primary"
              onClick={close}
            >
              <Dismiss />
            </button>
          </div>

          {canCreate ? <NoteComposer onCreated={refresh} /> : null}

          <div className="flex-1 overflow-y-auto px-1 pb-2">
            {error ? <Callout className="m-4" intent="danger" prefixIcon={<Alert />} title={error} /> : null}

            {showInitialSpinner ? (
              <div className="flex h-32 items-center justify-center">
                <ProgressCircular indeterminate dataTestId="notes-loading" />
              </div>
            ) : null}

            {hasLoaded && notes.length === 0 && !error ? (
              <div className="flex flex-col items-center gap-2 px-6 py-14 text-center">
                <Edit width="w-8" className="text-text-primary-50" />
                <p className="text-emphasis-300 text-text-primary">No notes yet</p>
                <p className="text-300 text-text-primary-50">
                  Notes posted here are visible to the whole team.
                  {canCreate ? " Add the first one above." : ""}
                </p>
              </div>
            ) : null}

            {dayGroups.map((group) => (
              <div key={group.label}>
                <div className="sticky top-0 z-10 bg-surface-elevated-base px-3 pt-3 pb-1">
                  {/* Mono + caps to match the per-note timestamps: all
                      temporal metadata shares one voice. */}
                  <span className="font-mono text-[11px] tracking-wide text-text-primary-50 uppercase">
                    {group.label}
                  </span>
                </div>
                {group.notes.map((note) => (
                  <NoteCard
                    key={note.id.toString()}
                    note={note}
                    isOwn={note.authorUsername === username}
                    canModerate={canModerate}
                    onChanged={refresh}
                  />
                ))}
              </div>
            ))}

            {hasMore ? (
              <div className="flex justify-center py-4">
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  text="Load more"
                  onClick={loadMore}
                  loading={isLoading}
                  disabled={isLoading}
                  testId="notes-load-more"
                />
              </div>
            ) : null}
          </div>
        </motion.aside>
      ) : null}
    </AnimatePresence>
  );
};

export default NotepadPanel;
