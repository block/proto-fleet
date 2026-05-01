import { describe, expect, it } from "vitest";
import {
  BUILT_IN_VIEWS,
  canonicalizeSearchParams,
  createDefaultSavedViewsRecord,
  createUserView,
  findView,
  getSavedViewsStorageKey,
  isBuiltInViewId,
  isSavedViewsRecordDefault,
  normalizeSavedViewsRecord,
  type SavedViewsRecord,
  VIEW_URL_PARAM,
  VIEWS_SCHEMA_VERSION,
  visibleBuiltInViews,
} from "./savedViews";

describe("savedViews helpers", () => {
  describe("canonicalizeSearchParams", () => {
    it("sorts keys and values, dropping the view key", () => {
      expect(canonicalizeSearchParams("status=offline&model=S21&view=foo&status=hashing")).toBe(
        "model=S21&status=hashing&status=offline",
      );
    });

    it("is idempotent", () => {
      const once = canonicalizeSearchParams("model=S21&status=offline");
      const twice = canonicalizeSearchParams(once);
      expect(twice).toBe(once);
    });

    it("treats URLSearchParams and string equivalently", () => {
      const params = new URLSearchParams("status=hashing&status=offline&model=S21");
      expect(canonicalizeSearchParams(params)).toBe(
        canonicalizeSearchParams("status=hashing&status=offline&model=S21"),
      );
    });
  });

  describe("normalizeSavedViewsRecord", () => {
    it("returns default record for non-object input", () => {
      expect(normalizeSavedViewsRecord(null)).toEqual(createDefaultSavedViewsRecord());
      expect(normalizeSavedViewsRecord("oops")).toEqual(createDefaultSavedViewsRecord());
      expect(normalizeSavedViewsRecord(42)).toEqual(createDefaultSavedViewsRecord());
    });

    it("drops malformed view entries and de-dupes by id", () => {
      const result = normalizeSavedViewsRecord({
        version: VIEWS_SCHEMA_VERSION,
        views: [
          { id: "a", name: "First", searchParams: "status=offline", createdAt: "2026-04-30T00:00:00.000Z" },
          { id: "a", name: "Duplicate", searchParams: "status=hashing", createdAt: "2026-04-30T00:00:00.000Z" },
          { id: "", name: "Empty id", searchParams: "" },
          null,
          { id: "b", name: "Second" },
          { id: "c", searchParams: "model=S21" },
        ],
        deletedBuiltInIds: ["needs-attention", "needs-attention", "offline"],
      });

      expect(result.views.map((view) => view.id)).toEqual(["a"]);
      expect(result.deletedBuiltInIds).toEqual(["needs-attention", "offline"]);
    });

    it("rejects entries that collide with built-in ids", () => {
      const result = normalizeSavedViewsRecord({
        version: VIEWS_SCHEMA_VERSION,
        views: [{ id: "all-miners", name: "Sneaky", searchParams: "" }],
        deletedBuiltInIds: [],
      });
      expect(result.views).toEqual([]);
    });

    it("canonicalizes searchParams on read", () => {
      const result = normalizeSavedViewsRecord({
        version: VIEWS_SCHEMA_VERSION,
        views: [
          {
            id: "a",
            name: "Mixed",
            searchParams: "status=offline&model=S21&view=ignored",
            createdAt: "2026-04-30T00:00:00.000Z",
          },
        ],
        deletedBuiltInIds: [],
      });
      expect(result.views[0].searchParams).toBe("model=S21&status=offline");
    });
  });

  describe("findView", () => {
    const record: SavedViewsRecord = {
      version: VIEWS_SCHEMA_VERSION,
      views: [{ id: "u1", name: "User one", searchParams: "model=S21", createdAt: "2026-04-30T00:00:00.000Z" }],
      deletedBuiltInIds: ["offline"],
    };

    it("finds user views", () => {
      expect(findView("u1", record)?.name).toBe("User one");
    });

    it("finds visible built-ins", () => {
      expect(findView("all-miners", record)?.name).toBe("All miners");
    });

    it("returns undefined for dismissed built-ins", () => {
      expect(findView("offline", record)).toBeUndefined();
    });

    it("returns undefined for unknown ids", () => {
      expect(findView("nope", record)).toBeUndefined();
    });
  });

  describe("visibleBuiltInViews", () => {
    it("filters dismissed built-ins, preserving order", () => {
      const record: SavedViewsRecord = {
        version: VIEWS_SCHEMA_VERSION,
        views: [],
        deletedBuiltInIds: ["needs-attention"],
      };
      expect(visibleBuiltInViews(record).map((view) => view.id)).toEqual(["all-miners", "offline"]);
    });
  });

  describe("createUserView", () => {
    it("canonicalizes searchParams and yields unique ids", () => {
      const a = createUserView({ name: "A", searchParams: "status=offline&view=ignore" });
      const b = createUserView({ name: "B", searchParams: "status=offline" });
      expect(a.searchParams).toBe("status=offline");
      expect(b.searchParams).toBe("status=offline");
      expect(a.id).not.toBe(b.id);
    });
  });

  describe("misc", () => {
    it("isBuiltInViewId reflects BUILT_IN_VIEWS", () => {
      for (const view of BUILT_IN_VIEWS) {
        expect(isBuiltInViewId(view.id)).toBe(true);
      }
      expect(isBuiltInViewId("not-a-builtin")).toBe(false);
    });

    it("isSavedViewsRecordDefault detects empty record", () => {
      expect(isSavedViewsRecordDefault(createDefaultSavedViewsRecord())).toBe(true);
      expect(
        isSavedViewsRecordDefault({
          version: VIEWS_SCHEMA_VERSION,
          views: [],
          deletedBuiltInIds: ["offline"],
        }),
      ).toBe(false);
    });

    it("getSavedViewsStorageKey scopes by username", () => {
      expect(getSavedViewsStorageKey("alice")).toBe("proto-fleet-miner-views:alice");
      expect(getSavedViewsStorageKey("")).toBe("proto-fleet-miner-views:anonymous");
    });

    it("VIEW_URL_PARAM matches the URL key used for active view", () => {
      expect(VIEW_URL_PARAM).toBe("view");
    });
  });
});
