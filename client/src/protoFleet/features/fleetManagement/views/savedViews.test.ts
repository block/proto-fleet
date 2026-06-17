import { describe, expect, it } from "vitest";
import {
  canonicalizeSearchParams,
  createDefaultSavedViewsRecord,
  createUserView,
  findView,
  getSavedViewsStorageKey,
  isSavedViewsRecordDefault,
  normalizeSavedViewsRecord,
  type SavedViewsRecord,
  TABS_WITH_SAVEABLE_STATE,
  VIEW_URL_PARAM,
  VIEWS_SCHEMA_VERSION,
} from "./savedViews";

describe("savedViews helpers", () => {
  describe("canonicalizeSearchParams", () => {
    it("sorts keys and values within the miners-tab whitelist, dropping the view key", () => {
      expect(canonicalizeSearchParams("status=offline&model=S21&view=foo&status=hashing", "miners")).toBe(
        "model=S21&status=hashing&status=offline",
      );
    });

    it("is idempotent within the same tab", () => {
      const once = canonicalizeSearchParams("model=S21&status=offline", "miners");
      const twice = canonicalizeSearchParams(once, "miners");
      expect(twice).toBe(once);
    });

    it("treats URLSearchParams and string equivalently", () => {
      const params = new URLSearchParams("status=hashing&status=offline&model=S21");
      expect(canonicalizeSearchParams(params, "miners")).toBe(
        canonicalizeSearchParams("status=hashing&status=offline&model=S21", "miners"),
      );
    });

    it("drops keys outside the active tab's whitelist", () => {
      expect(canonicalizeSearchParams("status=offline&page=3&debug=1&utm_source=test", "miners")).toBe(
        "status=offline",
      );
    });

    it("scopes filter keys to the active tab — miner-only keys drop on the racks tab", () => {
      // `status` is a miners-only filter; `zone` and `sort`/`dir` are
      // racks-owned URL state, so they survive.
      expect(canonicalizeSearchParams("status=offline&zone=DC1&sort=name", "racks")).toBe("sort=name&zone=DC1");
    });

    it("returns empty on tabs without a saveable surface", () => {
      expect(canonicalizeSearchParams("zone=DC1&sort=name", "buildings")).toBe("");
      expect(canonicalizeSearchParams("anything=goes", "sites")).toBe("");
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
          {
            id: "a",
            name: "First",
            tab: "miners",
            searchParams: "status=offline",
            createdAt: "2026-04-30T00:00:00.000Z",
          },
          {
            id: "a",
            name: "Duplicate",
            tab: "miners",
            searchParams: "status=hashing",
            createdAt: "2026-04-30T00:00:00.000Z",
          },
          { id: "", name: "Empty id", searchParams: "" },
          null,
          { id: "b", name: "Second", tab: "miners" },
          { id: "c", searchParams: "model=S21", tab: "miners" },
        ],
      });

      expect(result.views.map((view) => view.id)).toEqual(["a"]);
    });

    it("migrates v1 entries (no tab field) to tab=miners", () => {
      const result = normalizeSavedViewsRecord({
        version: 1,
        views: [{ id: "u1", name: "Legacy", searchParams: "status=offline", createdAt: "2026-04-30T00:00:00.000Z" }],
        deletedBuiltInIds: ["offline"],
      });
      expect(result.version).toBe(VIEWS_SCHEMA_VERSION);
      expect(result.views).toHaveLength(1);
      expect(result.views[0].tab).toBe("miners");
      expect(result.views[0].searchParams).toBe("status=offline");
    });

    it("falls back to miners when tab field is unrecognized", () => {
      const result = normalizeSavedViewsRecord({
        version: VIEWS_SCHEMA_VERSION,
        views: [{ id: "a", name: "Bad tab", tab: "garbage", searchParams: "status=offline" }],
      });
      expect(result.views[0].tab).toBe("miners");
    });

    it("preserves tab when set to a valid id", () => {
      const result = normalizeSavedViewsRecord({
        version: VIEWS_SCHEMA_VERSION,
        views: [{ id: "r1", name: "Rack view", tab: "racks", searchParams: "zone=DC1" }],
      });
      expect(result.views[0].tab).toBe("racks");
      expect(result.views[0].searchParams).toBe("zone=DC1");
    });

    it("re-canonicalizes searchParams against the view's own tab on read", () => {
      const result = normalizeSavedViewsRecord({
        version: VIEWS_SCHEMA_VERSION,
        views: [
          {
            id: "a",
            name: "Mixed",
            tab: "racks",
            // status is a miners-only key; should be dropped against the racks whitelist.
            searchParams: "status=offline&zone=DC1&view=ignored",
            createdAt: "2026-04-30T00:00:00.000Z",
          },
        ],
      });
      expect(result.views[0].searchParams).toBe("zone=DC1");
    });
  });

  describe("findView", () => {
    const record: SavedViewsRecord = {
      version: VIEWS_SCHEMA_VERSION,
      views: [
        { id: "u1", name: "User one", tab: "miners", searchParams: "model=S21", createdAt: "2026-04-30T00:00:00.000Z" },
      ],
    };

    it("finds user views by id", () => {
      expect(findView("u1", record)?.name).toBe("User one");
    });

    it("returns undefined for unknown ids", () => {
      expect(findView("nope", record)).toBeUndefined();
    });
  });

  describe("createUserView", () => {
    it("canonicalizes searchParams against the supplied tab and yields unique ids", () => {
      const a = createUserView({ name: "A", tab: "miners", searchParams: "status=offline&view=ignore" });
      const b = createUserView({ name: "B", tab: "miners", searchParams: "status=offline" });
      expect(a.searchParams).toBe("status=offline");
      expect(b.searchParams).toBe("status=offline");
      expect(a.tab).toBe("miners");
      expect(a.id).not.toBe(b.id);
    });
  });

  describe("misc", () => {
    it("TABS_WITH_SAVEABLE_STATE covers miners + racks", () => {
      expect(TABS_WITH_SAVEABLE_STATE.has("miners")).toBe(true);
      expect(TABS_WITH_SAVEABLE_STATE.has("racks")).toBe(true);
      expect(TABS_WITH_SAVEABLE_STATE.has("buildings")).toBe(false);
      expect(TABS_WITH_SAVEABLE_STATE.has("sites")).toBe(false);
    });

    it("isSavedViewsRecordDefault detects empty record", () => {
      expect(isSavedViewsRecordDefault(createDefaultSavedViewsRecord())).toBe(true);
      expect(
        isSavedViewsRecordDefault({
          version: VIEWS_SCHEMA_VERSION,
          views: [
            {
              id: "x",
              name: "x",
              tab: "miners",
              searchParams: "",
              createdAt: "2026-04-30T00:00:00.000Z",
            },
          ],
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
