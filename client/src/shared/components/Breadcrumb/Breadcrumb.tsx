import {
  forwardRef,
  Fragment,
  type KeyboardEvent as ReactKeyboardEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { Link, useNavigate } from "react-router-dom";
import clsx from "clsx";

import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

export interface BreadcrumbSibling {
  label: string;
  to: string;
  isActive: boolean;
  onSelect?: () => void;
}

/** Only the last segment may carry `siblings` — earlier segments are ancestor links. */
export interface BreadcrumbSegment {
  label: string;
  to?: string;
  siblings?: BreadcrumbSibling[];
}

export interface BreadcrumbProps {
  segments: BreadcrumbSegment[];
  testId?: string;
}

const Breadcrumb = ({ segments, testId = "breadcrumb" }: BreadcrumbProps) => {
  const [menuOpen, setMenuOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();

  useEscapeDismiss(
    menuOpen
      ? () => {
          setMenuOpen(false);
          triggerRef.current?.focus();
        }
      : undefined,
  );

  useEffect(() => {
    if (!menuOpen || !menuRef.current) return;
    const firstItem = menuRef.current.querySelector<HTMLButtonElement>("[role='menuitem']");
    firstItem?.focus();
  }, [menuOpen]);

  const handleSelect = useCallback(
    (sibling: BreadcrumbSibling) => {
      setMenuOpen(false);
      triggerRef.current?.focus();
      sibling.onSelect?.();
      navigate(sibling.to);
    },
    [navigate],
  );

  const handleMenuKeyDown = useCallback((e: ReactKeyboardEvent) => {
    const menu = menuRef.current;
    if (!menu) return;
    const items = Array.from(menu.querySelectorAll<HTMLButtonElement>("[role='menuitem']"));
    const idx = items.indexOf(e.target as HTMLButtonElement);
    if (idx < 0) return;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      items[(idx + 1) % items.length].focus();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      items[(idx - 1 + items.length) % items.length].focus();
    } else if (e.key === "Home") {
      e.preventDefault();
      items[0].focus();
    } else if (e.key === "End") {
      e.preventDefault();
      items[items.length - 1].focus();
    } else if (e.key === "Tab") {
      e.preventDefault();
      setMenuOpen(false);
      triggerRef.current?.focus();
    }
  }, []);

  return (
    <nav className="flex items-center gap-2 text-300" data-testid={testId} aria-label="Breadcrumb">
      {segments.map((seg, i) => {
        const isLast = i === segments.length - 1;
        const hasSiblings = isLast && seg.siblings && seg.siblings.length > 0;

        return (
          <Fragment key={`${seg.label}-${i}`}>
            {i > 0 ? <span className="text-text-primary-70">/</span> : null}
            {!isLast && seg.to ? (
              <Link
                to={seg.to}
                className="text-text-primary underline hover:opacity-80"
                data-testid={`${testId}-link-${i}`}
              >
                {seg.label}
              </Link>
            ) : hasSiblings ? (
              <span className="relative">
                <button
                  ref={triggerRef}
                  type="button"
                  onClick={() => setMenuOpen((prev) => !prev)}
                  className="inline-flex items-center gap-1 text-text-primary hover:text-text-primary-70"
                  data-testid={`${testId}-switcher`}
                  aria-haspopup="menu"
                  aria-expanded={menuOpen}
                >
                  <span>{seg.label}</span>
                  <ChevronDown width={iconSizes.xSmall} />
                </button>
                {menuOpen ? (
                  <SiblingMenu
                    ref={menuRef}
                    siblings={seg.siblings!}
                    onSelect={handleSelect}
                    onDismiss={() => {
                      setMenuOpen(false);
                      triggerRef.current?.focus();
                    }}
                    onKeyDown={handleMenuKeyDown}
                    testId={`${testId}-menu`}
                  />
                ) : null}
              </span>
            ) : (
              <span className="text-text-primary" data-testid={`${testId}-current`}>
                {seg.label}
              </span>
            )}
          </Fragment>
        );
      })}
    </nav>
  );
};

interface SiblingMenuProps {
  siblings: BreadcrumbSibling[];
  onSelect: (sibling: BreadcrumbSibling) => void;
  onDismiss: () => void;
  onKeyDown: (e: ReactKeyboardEvent) => void;
  testId: string;
}

const SiblingMenu = forwardRef<HTMLDivElement, SiblingMenuProps>(
  ({ siblings, onSelect, onDismiss, onKeyDown, testId }, ref) => (
    <>
      <div className="fixed inset-0 z-20" role="presentation" onClick={onDismiss} />
      <div
        ref={ref}
        role="menu"
        data-testid={testId}
        onKeyDown={onKeyDown}
        className="absolute top-full left-0 z-30 mt-1.5 max-h-72 min-w-44 overflow-y-auto rounded-lg border border-border-5 bg-surface-elevated-base p-1.5 shadow-300"
      >
        {siblings.map((sib) => (
          <button
            key={sib.to}
            type="button"
            role="menuitem"
            tabIndex={-1}
            onClick={() => onSelect(sib)}
            className={clsx(
              "flex w-full items-center gap-3 rounded-lg p-3 text-left text-300 hover:bg-surface-5",
              sib.isActive ? "font-medium text-text-primary" : "text-text-primary",
            )}
            data-testid={`${testId}-item-${sib.label}`}
          >
            <span
              className={clsx(
                "flex size-5 shrink-0 items-center justify-center rounded-full border-[1.5px]",
                sib.isActive ? "border-transparent bg-intent-warning-fill" : "border-border-20",
              )}
            >
              {sib.isActive ? <span className="size-2.5 rounded-full bg-white" /> : null}
            </span>
            <span className="truncate">{sib.label}</span>
          </button>
        ))}
      </div>
    </>
  ),
);

SiblingMenu.displayName = "SiblingMenu";

export default Breadcrumb;
