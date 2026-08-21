/**
 * Page-scroll chrome width.
 *
 * In the desktop Fleet shell the page (AppLayout's scroll container) is the
 * single scroll container for BOTH axes. Wide tables scroll the page
 * horizontally, so any chrome that should stay pinned to the left edge during
 * that scroll (filter rows, headers, counts, action bars) needs:
 *
 *   1. `sticky left-0` — pin to the scroll port's left edge, and
 *   2. an explicit viewport-minus-sidebar width — so the element is narrower
 *      than its (max-content) containing block and therefore has room to
 *      slide. Without the width it fills the containing block and can't move,
 *      so it scrolls away with the table.
 *
 * The subtracted amounts mirror AppLayout's sidebar offsets (`laptop:left-16`
 * = 64px, `desktop:left-50` = 200px); phone has no inline sidebar. We also
 * subtract `--content-scroll-gutter` — the vertical scrollbar width that
 * `100vw` counts but the scroll container's client area does not. Without it
 * the chrome is one-scrollbar too wide and over-travels (~15px) at the far end
 * of a horizontal scroll. AppLayout measures and publishes that variable; it
 * resolves to 0 for overlay scrollbars.
 *
 * Pair this with a desktop-only `laptop:w-max laptop:min-w-full` ancestor so
 * the containing block grows to the table width where page-wide horizontal
 * table scroll is still used.
 */
export const PAGE_SCROLL_CHROME_WIDTH =
  "w-[calc(100vw-var(--content-scroll-gutter,0px))] laptop:w-[calc(100vw-theme(spacing.1)*16-var(--content-scroll-gutter,0px))] desktop:w-[calc(100vw-theme(spacing.1)*50-var(--content-scroll-gutter,0px))]";
