import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { notesClient } from "./clients";
import { mergeHeadPage, useNotesFeed } from "./useNotesFeed";
import { ListNotesResponseSchema, type Note, NoteSchema } from "@/protoFleet/api/generated/notes/v1/notes_pb";

vi.mock("./clients", () => ({
  notesClient: {
    listNotes: vi.fn(),
  },
}));

const mockHandleAuthErrors = vi.fn(({ onError }) => onError?.(new Error("auth error")));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: mockHandleAuthErrors,
  })),
}));

// seconds encodes the feed position; id breaks ties exactly like the
// server's (created_at, id) keyset. An edited note must carry a later
// updatedSeconds — the server's updated_at trigger guarantees content
// never changes without it, and the merge's change detection relies
// on that invariant.
function makeNote(id: number, seconds: number, content = `note ${id}`, updatedSeconds = seconds): Note {
  return create(NoteSchema, {
    id: BigInt(id),
    content,
    authorUsername: "alice",
    createdAt: create(TimestampSchema, { seconds: BigInt(seconds), nanos: 0 }),
    updatedAt: create(TimestampSchema, { seconds: BigInt(updatedSeconds), nanos: 0 }),
  });
}

function mockListResponse(notes: Note[], nextPageToken = "") {
  return create(ListNotesResponseSchema, { notes, nextPageToken });
}

describe("mergeHeadPage", () => {
  // Feed order is newest-first; the head page covers ids 5..3.
  const held = [makeNote(5, 500), makeNote(4, 400), makeNote(3, 300), makeNote(2, 200), makeNote(1, 100)];

  it("prepends rows strictly newer than the current head", () => {
    const head = [makeNote(6, 600), makeNote(5, 500), makeNote(4, 400)];
    const merged = mergeHeadPage(held, head);
    expect(merged.map((n) => n.id)).toEqual([6n, 5n, 4n, 3n, 2n, 1n]);
  });

  it("replaces held copies with the head's version to pick up edits", () => {
    const edited = makeNote(4, 400, "edited content", 450);
    const head = [makeNote(5, 500), edited, makeNote(3, 300)];
    const merged = mergeHeadPage(held, head);
    expect(merged.find((n) => n.id === 4n)?.content).toBe("edited content");
    expect(merged.map((n) => n.id)).toEqual([5n, 4n, 3n, 2n, 1n]);
  });

  it("drops held rows inside the head window that the head no longer carries", () => {
    // Note 4 was deleted upstream: the head window now spans 5..2.
    const head = [makeNote(5, 500), makeNote(3, 300), makeNote(2, 200)];
    const merged = mergeHeadPage(held, head);
    expect(merged.map((n) => n.id)).toEqual([5n, 3n, 2n, 1n]);
  });

  it("keeps rows older than the head window untouched", () => {
    const head = [makeNote(5, 500), makeNote(4, 400)];
    const merged = mergeHeadPage(held, head);
    expect(merged.map((n) => n.id)).toEqual([5n, 4n, 3n, 2n, 1n]);
  });

  it("breaks created_at ties by id like the server keyset", () => {
    // Held rows 3 and 2 share a timestamp; the window floor is (300, 3),
    // so id 2 (same time, lower id) is older-than-window and survives.
    const tiedHeld = [makeNote(3, 300), makeNote(2, 300), makeNote(1, 100)];
    const head = [makeNote(4, 300), makeNote(3, 300)];
    const merged = mergeHeadPage(tiedHeld, head);
    expect(merged.map((n) => n.id)).toEqual([4n, 3n, 2n, 1n]);
  });

  it("returns an empty feed when the head page is empty", () => {
    expect(mergeHeadPage(held, [])).toEqual([]);
  });

  it("drops rows below the window when the head is the whole feed", () => {
    // No continuation token: notes 3..1 were deleted upstream and the
    // feed now ends at the head window floor.
    const head = [makeNote(5, 500), makeNote(4, 400)];
    const merged = mergeHeadPage(held, head, true);
    expect(merged.map((n) => n.id)).toEqual([5n, 4n]);
  });

  it("returns the previous array reference when the head changes nothing", () => {
    // The poll tick runs this inside a setState updater; returning the
    // same reference is what lets React skip the re-render.
    const sameHead = [makeNote(5, 500), makeNote(4, 400), makeNote(3, 300), makeNote(2, 200), makeNote(1, 100)];
    expect(mergeHeadPage(held, sameHead)).toBe(held);
  });

  it("returns the previous reference when an empty feed stays empty", () => {
    const empty: Note[] = [];
    expect(mergeHeadPage(empty, [])).toBe(empty);
  });

  it("returns the previous reference when a complete head changes nothing", () => {
    const sameHead = [makeNote(5, 500), makeNote(4, 400), makeNote(3, 300), makeNote(2, 200), makeNote(1, 100)];
    expect(mergeHeadPage(held, sameHead, true)).toBe(held);
  });

  it("returns a new array when only a note's updated_at changed", () => {
    const head = [
      makeNote(5, 500, "note 5", 550),
      makeNote(4, 400),
      makeNote(3, 300),
      makeNote(2, 200),
      makeNote(1, 100),
    ];
    expect(mergeHeadPage(held, head)).not.toBe(held);
  });
});

describe("useNotesFeed", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // The hook deliberately has no fetch-on-mount: the panel drives the
  // initial load through usePoll when it opens. Tests stand in for the
  // panel by calling refresh() explicitly.
  it("refresh loads the first page and reports hasLoaded", async () => {
    vi.mocked(notesClient.listNotes).mockResolvedValue(mockListResponse([makeNote(2, 200), makeNote(1, 100)]));

    const { result } = renderHook(() => useNotesFeed({ pageSize: 25 }));
    expect(result.current.hasLoaded).toBe(false);

    act(() => {
      result.current.refresh();
    });

    await waitFor(() => {
      expect(result.current.hasLoaded).toBe(true);
    });

    expect(notesClient.listNotes).toHaveBeenCalledWith({ pageSize: 25, pageToken: "" });
    expect(result.current.notes).toHaveLength(2);
    expect(result.current.hasMore).toBe(false);
  });

  it("loadMore appends the next page using the returned token", async () => {
    vi.mocked(notesClient.listNotes)
      .mockResolvedValueOnce(mockListResponse([makeNote(2, 200)], "token-2"))
      .mockResolvedValueOnce(mockListResponse([makeNote(1, 100)], ""));

    const { result } = renderHook(() => useNotesFeed({ pageSize: 1 }));

    act(() => {
      result.current.refresh();
    });

    await waitFor(() => {
      expect(result.current.hasMore).toBe(true);
    });

    act(() => {
      result.current.loadMore();
    });

    await waitFor(() => {
      expect(result.current.notes).toHaveLength(2);
    });

    expect(notesClient.listNotes).toHaveBeenLastCalledWith({ pageSize: 1, pageToken: "token-2" });
    expect(result.current.hasMore).toBe(false);
    expect(result.current.notes.map((n) => n.id)).toEqual([2n, 1n]);
  });

  it("refreshHead merges the head page without collapsing loaded pages", async () => {
    vi.mocked(notesClient.listNotes)
      .mockResolvedValueOnce(mockListResponse([makeNote(3, 300), makeNote(2, 200)], "token-2"))
      .mockResolvedValueOnce(mockListResponse([makeNote(1, 100)], ""))
      // Poll tick: note 4 arrived and note 3 was deleted upstream, so
      // the fresh head window spans (400 .. 200). The continuation
      // token (always present on a full page) says rows exist below
      // the window, so note 1 must survive untouched.
      .mockResolvedValueOnce(mockListResponse([makeNote(4, 400), makeNote(2, 200)], "token-head"));

    const { result } = renderHook(() => useNotesFeed({ pageSize: 2 }));

    act(() => {
      result.current.refresh();
    });
    await waitFor(() => {
      expect(result.current.hasMore).toBe(true);
    });
    act(() => {
      result.current.loadMore();
    });
    await waitFor(() => {
      expect(result.current.notes).toHaveLength(3);
    });

    await act(async () => {
      await result.current.refreshHead();
    });

    expect(result.current.notes.map((n) => n.id)).toEqual([4n, 2n, 1n]);
    expect(result.current.hasMore).toBe(false);
  });

  it("a head page with no continuation token replaces the feed and clears the cursor", async () => {
    vi.mocked(notesClient.listNotes)
      .mockResolvedValueOnce(mockListResponse([makeNote(4, 400), makeNote(3, 300)], "token-2"))
      .mockResolvedValueOnce(mockListResponse([makeNote(2, 200), makeNote(1, 100)], "token-3"))
      // Poll tick: everything below note 4 was deleted upstream, so
      // the head page carries the entire feed and no token.
      .mockResolvedValueOnce(mockListResponse([makeNote(4, 400)]));

    const { result } = renderHook(() => useNotesFeed({ pageSize: 2 }));

    act(() => {
      result.current.refresh();
    });
    await waitFor(() => {
      expect(result.current.hasMore).toBe(true);
    });
    act(() => {
      result.current.loadMore();
    });
    await waitFor(() => {
      expect(result.current.notes).toHaveLength(4);
    });
    expect(result.current.hasMore).toBe(true);

    await act(async () => {
      await result.current.refreshHead();
    });

    expect(result.current.notes.map((n) => n.id)).toEqual([4n]);
    expect(result.current.hasMore).toBe(false);

    // The cleared cursor makes a follow-up Load more a no-op instead
    // of appending a stale page below the now-complete feed.
    act(() => {
      result.current.loadMore();
    });
    expect(notesClient.listNotes).toHaveBeenCalledTimes(3);
  });

  it("an empty head page clears the feed and the cursor", async () => {
    vi.mocked(notesClient.listNotes)
      .mockResolvedValueOnce(mockListResponse([makeNote(1, 100)], "token-x"))
      .mockResolvedValueOnce(mockListResponse([]));

    const { result } = renderHook(() => useNotesFeed({ pageSize: 1 }));

    act(() => {
      result.current.refresh();
    });
    await waitFor(() => {
      expect(result.current.hasMore).toBe(true);
    });

    await act(async () => {
      await result.current.refreshHead();
    });

    expect(result.current.notes).toEqual([]);
    expect(result.current.hasMore).toBe(false);
  });

  it("an initial load through refreshHead seeds the Load more cursor", async () => {
    // The panel's first fetch goes through usePoll → refreshHead, not
    // refresh(), so a deeper-than-one-page feed must still surface
    // Load more after that first tick.
    vi.mocked(notesClient.listNotes)
      .mockResolvedValueOnce(mockListResponse([makeNote(3, 300), makeNote(2, 200)], "token-2"))
      .mockResolvedValueOnce(mockListResponse([makeNote(1, 100)], ""));

    const { result } = renderHook(() => useNotesFeed({ pageSize: 2 }));

    await act(async () => {
      await result.current.refreshHead();
    });

    expect(result.current.hasLoaded).toBe(true);
    expect(result.current.hasMore).toBe(true);

    act(() => {
      result.current.loadMore();
    });
    await waitFor(() => {
      expect(result.current.notes).toHaveLength(3);
    });

    expect(notesClient.listNotes).toHaveBeenLastCalledWith({ pageSize: 2, pageToken: "token-2" });
    expect(result.current.hasMore).toBe(false);
  });

  it("a later head tick with a continuation token leaves the cursor alone", async () => {
    // After the initial load, the held cursor tracks the bottom of the
    // accumulated list; a head token only describes rows below the
    // head window, which the list already holds.
    vi.mocked(notesClient.listNotes)
      .mockResolvedValueOnce(mockListResponse([makeNote(2, 200), makeNote(1, 100)], ""))
      .mockResolvedValueOnce(mockListResponse([makeNote(3, 300), makeNote(2, 200)], "token-stale"));

    const { result } = renderHook(() => useNotesFeed({ pageSize: 2 }));

    act(() => {
      result.current.refresh();
    });
    await waitFor(() => {
      expect(result.current.hasLoaded).toBe(true);
    });
    expect(result.current.hasMore).toBe(false);

    await act(async () => {
      await result.current.refreshHead();
    });

    expect(result.current.notes.map((n) => n.id)).toEqual([3n, 2n, 1n]);
    expect(result.current.hasMore).toBe(false);
  });

  it("surfaces list errors through the shared auth handler", async () => {
    vi.mocked(notesClient.listNotes).mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useNotesFeed());

    act(() => {
      result.current.refresh();
    });

    await waitFor(() => {
      expect(result.current.error).not.toBeNull();
    });
    expect(mockHandleAuthErrors).toHaveBeenCalled();
    expect(result.current.hasLoaded).toBe(false);
  });

  it("refreshHead surfaces a failure before anything has loaded", async () => {
    vi.mocked(notesClient.listNotes).mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useNotesFeed());

    await act(async () => {
      await result.current.refreshHead();
    });

    expect(result.current.error).not.toBeNull();
    expect(result.current.hasLoaded).toBe(false);
  });

  it("refreshHead stays silent on failures after a successful load", async () => {
    vi.mocked(notesClient.listNotes)
      .mockResolvedValueOnce(mockListResponse([makeNote(1, 100)]))
      .mockRejectedValueOnce(new Error("boom"));

    const { result } = renderHook(() => useNotesFeed());

    await act(async () => {
      await result.current.refreshHead();
    });
    expect(result.current.hasLoaded).toBe(true);

    await act(async () => {
      await result.current.refreshHead();
    });

    expect(result.current.error).toBeNull();
    expect(result.current.notes.map((n) => n.id)).toEqual([1n]);
  });

  it("a successful refreshHead clears the error from a failed first load", async () => {
    vi.mocked(notesClient.listNotes)
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce(mockListResponse([makeNote(1, 100)]));

    const { result } = renderHook(() => useNotesFeed());

    await act(async () => {
      await result.current.refreshHead();
    });
    expect(result.current.error).not.toBeNull();

    await act(async () => {
      await result.current.refreshHead();
    });

    expect(result.current.error).toBeNull();
    expect(result.current.hasLoaded).toBe(true);
    expect(result.current.notes.map((n) => n.id)).toEqual([1n]);
  });
});
