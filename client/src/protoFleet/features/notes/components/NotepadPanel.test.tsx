import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import NotepadPanel from "./NotepadPanel";
import { type Note, NoteSchema } from "@/protoFleet/api/generated/notes/v1/notes_pb";
import { useNotesFeed } from "@/protoFleet/api/useNotesFeed";
import { useHasPermission, useIsNotepadOpen, useSetNotepadOpen, useUsername } from "@/protoFleet/store";
import { __resetEscapeStackForTests } from "@/shared/hooks/useEscapeDismiss";

const mockSetNotepadOpen = vi.fn();

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
  useIsNotepadOpen: vi.fn(),
  useSetNotepadOpen: vi.fn(),
  useUsername: vi.fn(),
}));

vi.mock("@/protoFleet/api/useNotesFeed", () => ({
  useNotesFeed: vi.fn(),
}));

vi.mock("@/protoFleet/api/notes", () => ({
  MAX_NOTE_CONTENT_LENGTH: 4096,
  useNotes: () => ({
    createNote: vi.fn(),
    updateNote: vi.fn(),
    deleteNote: vi.fn(),
  }),
}));

function makeNote(id: number, author: string, opts: { edited?: boolean } = {}): Note {
  return create(NoteSchema, {
    id: BigInt(id),
    content: `note ${id}`,
    authorUsername: author,
    createdAt: create(TimestampSchema, { seconds: 100n, nanos: 0 }),
    updatedAt: create(TimestampSchema, { seconds: opts.edited ? 200n : 100n, nanos: 0 }),
  });
}

type Feed = ReturnType<typeof useNotesFeed>;

function mockFeed(overrides: Partial<Feed> = {}): Feed {
  return {
    notes: [],
    isLoading: false,
    hasLoaded: true,
    error: null,
    hasMore: false,
    loadMore: vi.fn(),
    refresh: vi.fn(),
    refreshHead: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

// Permissions for the common case: read + create, no moderation.
function grantPermissions(keys: string[]) {
  vi.mocked(useHasPermission).mockImplementation((key: string) => keys.includes(key));
}

describe("NotepadPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    __resetEscapeStackForTests();
    vi.mocked(useIsNotepadOpen).mockReturnValue(true);
    vi.mocked(useSetNotepadOpen).mockReturnValue(mockSetNotepadOpen);
    vi.mocked(useUsername).mockReturnValue("alice");
    vi.mocked(useNotesFeed).mockReturnValue(mockFeed());
    grantPermissions(["note:read", "note:create"]);
  });

  it("renders nothing without note:read even when the store flag is open", () => {
    grantPermissions([]);
    render(<NotepadPanel />);
    expect(screen.queryByTestId("notepad-panel")).not.toBeInTheDocument();
  });

  it("renders nothing while the store flag is closed", () => {
    vi.mocked(useIsNotepadOpen).mockReturnValue(false);
    render(<NotepadPanel />);
    expect(screen.queryByTestId("notepad-panel")).not.toBeInTheDocument();
  });

  it("shows the panel with composer when open with note:create", () => {
    render(<NotepadPanel />);
    expect(screen.getByTestId("notepad-panel")).toBeInTheDocument();
    expect(screen.getByText("Notepad")).toBeVisible();
    expect(screen.getByTestId("note-composer")).toBeInTheDocument();
  });

  it("hides the composer without note:create", () => {
    grantPermissions(["note:read"]);
    render(<NotepadPanel />);
    expect(screen.getByTestId("notepad-panel")).toBeInTheDocument();
    expect(screen.queryByTestId("note-composer")).not.toBeInTheDocument();
  });

  it("closes via the close button and via Escape", () => {
    render(<NotepadPanel />);

    fireEvent.click(screen.getByTestId("notepad-close"));
    expect(mockSetNotepadOpen).toHaveBeenCalledWith(false);

    mockSetNotepadOpen.mockClear();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(mockSetNotepadOpen).toHaveBeenCalledWith(false);
  });

  it("offers edit only on own notes and delete on others' only with note:manage", () => {
    vi.mocked(useNotesFeed).mockReturnValue(mockFeed({ notes: [makeNote(1, "alice"), makeNote(2, "bob")] }));

    const { rerender } = render(<NotepadPanel />);
    let cards = screen.getAllByTestId("note-card");
    expect(within(cards[0]).getByTestId("note-edit")).toBeInTheDocument();
    expect(within(cards[0]).getByTestId("note-delete")).toBeInTheDocument();
    expect(within(cards[1]).queryByTestId("note-edit")).not.toBeInTheDocument();
    expect(within(cards[1]).queryByTestId("note-delete")).not.toBeInTheDocument();

    grantPermissions(["note:read", "note:create", "note:manage"]);
    rerender(<NotepadPanel />);
    cards = screen.getAllByTestId("note-card");
    expect(within(cards[1]).getByTestId("note-delete")).toBeInTheDocument();
    expect(within(cards[1]).queryByTestId("note-edit")).not.toBeInTheDocument();
  });

  it("marks edited notes", () => {
    vi.mocked(useNotesFeed).mockReturnValue(
      mockFeed({ notes: [makeNote(1, "alice", { edited: true }), makeNote(2, "alice")] }),
    );

    render(<NotepadPanel />);
    const cards = screen.getAllByTestId("note-card");
    expect(within(cards[0]).getByText(/\(edited\)/)).toBeInTheDocument();
    expect(within(cards[1]).queryByText(/\(edited\)/)).not.toBeInTheDocument();
  });

  it("shows the empty state once loaded with no notes", () => {
    render(<NotepadPanel />);
    expect(screen.getByText(/No notes yet/)).toBeInTheDocument();
  });

  it("shows the spinner until the first load resolves", () => {
    vi.mocked(useNotesFeed).mockReturnValue(mockFeed({ hasLoaded: false }));

    render(<NotepadPanel />);
    expect(screen.getByTestId("notes-loading")).toBeInTheDocument();
  });

  it("shows the error callout instead of the spinner when the first load fails", () => {
    vi.mocked(useNotesFeed).mockReturnValue(mockFeed({ hasLoaded: false, error: "Failed to load notes" }));

    render(<NotepadPanel />);
    expect(screen.getByText("Failed to load notes")).toBeInTheDocument();
    expect(screen.queryByTestId("notes-loading")).not.toBeInTheDocument();
  });

  it("shows Load more when the feed has another page", () => {
    const feed = mockFeed({ notes: [makeNote(1, "alice")], hasMore: true });
    vi.mocked(useNotesFeed).mockReturnValue(feed);

    render(<NotepadPanel />);
    fireEvent.click(screen.getByTestId("notes-load-more"));
    expect(feed.loadMore).toHaveBeenCalled();
  });
});
