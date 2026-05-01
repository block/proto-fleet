import {
  createContext,
  type CSSProperties,
  type HTMLAttributes,
  type ReactNode,
  type Ref,
  useContext,
  useMemo,
} from "react";
import clsx from "clsx";

type TabStripContextValue = {
  activeId?: string;
  onSelect: (id: string) => void;
};

const TabStripContext = createContext<TabStripContextValue | null>(null);

const useTabStripContext = () => {
  const ctx = useContext(TabStripContext);
  if (!ctx) {
    throw new Error("TabStripItem must be rendered inside <TabStrip>");
  }
  return ctx;
};

type TabStripProps = {
  children: ReactNode;
  activeId?: string;
  onSelect: (id: string) => void;
  className?: string;
  ariaLabel?: string;
  /**
   * Rendered inside the tablist container after the tab items, sharing the
   * underline. Use for trailing actions that visually belong on the tab row
   * but aren't selectable tabs (e.g. "Save view").
   */
  trailing?: ReactNode;
};

/**
 * Controlled, content-less tab bar. Use `<TabStripItem>` as children.
 * For tabs that own panel content too, use `Tabs` from this same module.
 *
 * Note on ARIA: this is a navigation-style tab strip, not an ARIA tabs widget.
 * Children may include non-tab controls (e.g. "+ New view"), so we don't put
 * `role="tablist"` on the container, and items use plain buttons rather than
 * `role="tab"` (we don't implement the ARIA tabs keyboard model — roving
 * tabindex, arrow keys, Home/End). The active item is announced via
 * `aria-current="page"` instead.
 */
const TabStrip = ({ children, activeId, onSelect, className, ariaLabel, trailing }: TabStripProps) => {
  const value = useMemo<TabStripContextValue>(() => ({ activeId, onSelect }), [activeId, onSelect]);
  return (
    <TabStripContext.Provider value={value}>
      <nav
        aria-label={ariaLabel}
        className={clsx("flex w-full items-end space-x-2 border-b-2 border-border-5 whitespace-nowrap", className)}
      >
        {children}
        {trailing ? <div className="ml-auto flex items-end gap-4">{trailing}</div> : null}
      </nav>
    </TabStripContext.Provider>
  );
};

export type TabStripItemTone = "default" | "warning";

const TONE_ACTIVE_TEXT: Record<TabStripItemTone, string> = {
  default: "text-text-emphasis",
  warning: "text-intent-warning-50",
};

const TONE_UNDERLINE: Record<TabStripItemTone, string> = {
  default: "bg-text-emphasis",
  warning: "bg-intent-warning-50",
};

export type TabStripItemProps = {
  id: string;
  label: ReactNode;
  /** Rendered before the label inside the tab cell, outside the activate button. */
  leading?: ReactNode;
  /** Rendered after the label inside the tab cell, outside the activate button. */
  trailing?: ReactNode;
  /** When true, the tab is rendered but cannot be activated. */
  disabled?: boolean;
  testId?: string;
  /**
   * Color treatment for the tab. "warning" swaps the active text/underline
   * colors to intent-warning — used to signal a dirty/modified state.
   * Only applies when the tab is the active one.
   */
  tone?: TabStripItemTone;
  /** Forwarded to the tab cell wrapper — e.g. dnd-kit's `setNodeRef`. */
  wrapperRef?: Ref<HTMLDivElement>;
  /** Spread onto the tab cell wrapper — e.g. dnd-kit's `attributes`. */
  wrapperProps?: HTMLAttributes<HTMLDivElement>;
  /** Style on the tab cell wrapper — kept separate so consumers can pass dnd transforms. */
  wrapperStyle?: CSSProperties;
};

export const TabStripItem = ({
  id,
  label,
  leading,
  trailing,
  disabled,
  testId,
  tone = "default",
  wrapperRef,
  wrapperProps,
  wrapperStyle,
}: TabStripItemProps) => {
  const { activeId, onSelect } = useTabStripContext();
  const isActive = id === activeId;
  const activeTextClass = TONE_ACTIVE_TEXT[tone];
  const underlineClass = TONE_UNDERLINE[tone];

  return (
    <div
      ref={wrapperRef}
      style={wrapperStyle}
      {...wrapperProps}
      className={clsx(
        "relative flex items-center gap-1 px-2 first:pl-0",
        isActive ? activeTextClass : "text-text-primary-70",
        wrapperProps?.className,
      )}
      data-testid={testId}
      data-active={isActive ? "true" : undefined}
    >
      {leading}
      <button
        type="button"
        aria-current={isActive ? "page" : undefined}
        disabled={disabled}
        onClick={() => {
          if (!disabled) onSelect(id);
        }}
        className={clsx(
          "relative pb-2 text-300 outline-none focus-visible:underline",
          isActive ? activeTextClass : "hover:text-text-primary",
          disabled && "cursor-not-allowed opacity-50",
        )}
        data-testid={testId ? `${testId}-activate` : undefined}
      >
        {isActive ? (
          <span aria-hidden className={clsx("absolute right-0 bottom-[-0.1rem] left-0 h-0.5", underlineClass)} />
        ) : null}
        <span className="relative">{label}</span>
      </button>
      {trailing}
    </div>
  );
};

export default TabStrip;
