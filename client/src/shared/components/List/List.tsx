import {
  ChangeEvent,
  type CSSProperties,
  type MouseEvent,
  type MutableRefObject,
  ReactNode,
  Ref,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import clsx from "clsx";
import {
  closestCenter,
  DndContext,
  DragEndEvent,
  KeyboardSensor,
  PointerSensor,
  type UniqueIdentifier,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import Button, { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Filters from "@/shared/components/List/Filters";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import ListActions from "@/shared/components/List/ListActions";
import {
  ColConfig,
  ColTitles,
  ListAction,
  resolveListActionValue,
  SORT_ASC,
  SORT_DESC,
  SortDirection,
} from "@/shared/components/List/types";
import { PopoverProvider } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Radio from "@/shared/components/Radio";
import SortIndicator from "@/shared/components/SortIndicator";
import { Breakpoint, breakpoints } from "@/shared/constants/breakpoints";
import { useStickyState } from "@/shared/hooks/useStickyState";

const INTERACTIVE_ELEMENT_SELECTOR =
  'button, a, input, select, textarea, [role="button"], [role="link"], [data-interactive]';

const getCssPixelValue = (variable: string) => {
  const value = window.getComputedStyle(document.body).getPropertyValue(variable);
  const parsedValue = Number.parseFloat(value);
  return Number.isFinite(parsedValue) ? parsedValue : 0;
};

const getActiveBreakpoint = (): Breakpoint => {
  const width = window.innerWidth;
  const phoneMaxWidth = getCssPixelValue("--phone-max-width");
  const tabletMaxWidth = getCssPixelValue("--tablet-max-width");
  const laptopMaxWidth = getCssPixelValue("--laptop-max-width");

  if (width > laptopMaxWidth) return breakpoints.desktop;
  if (width > tabletMaxWidth) return breakpoints.laptop;
  if (width > phoneMaxWidth) return breakpoints.tablet;
  return breakpoints.phone;
};

const getOverflowMeasurementWidth = (
  table: HTMLTableElement,
  paddingRight: Partial<Record<Breakpoint, string>> | undefined,
  hasHorizontalOverflow: boolean,
) => {
  if (!paddingRight || !hasHorizontalOverflow) {
    return table.offsetWidth;
  }

  const activePadding = Number.parseFloat(paddingRight[getActiveBreakpoint()] ?? "0");
  return table.offsetWidth - (Number.isFinite(activePadding) ? activePadding : 0);
};

type SelectionMode = "none" | "all" | "subset";

type ControlledSelectionModeProps<ItemKeyValueType> = {
  customSelectedItems: ItemKeyValueType[];
  customSetSelectedItems: (selected: ItemKeyValueType[]) => void;
  /**
   * Callback when selection mode changes.
   * Called with "all" when Select All is clicked with no filters,
   * "subset" for individual selections or Select All with filters,
   * "none" when selection is cleared.
   */
  onSelectionModeChange: (mode: SelectionMode) => void;
  /**
   * Controlled selection mode value.
   * Use with customSelectedItems/customSetSelectedItems when the parent owns
   * selection state and needs to keep the list's derived mode in sync.
   */
  customSelectionMode: SelectionMode;
};

type UncontrolledSelectionModeProps<ItemKeyValueType> = {
  customSelectedItems?: ItemKeyValueType[];
  customSetSelectedItems?: (selected: ItemKeyValueType[]) => void;
  /**
   * Callback when selection mode changes.
   * Called with "all" when Select All is clicked with no filters,
   * "subset" for individual selections or Select All with filters,
   * "none" when selection is cleared.
   */
  onSelectionModeChange?: (mode: SelectionMode) => void;
  customSelectionMode?: undefined;
};

type RowReorderDisabledProps = {
  onRowReorder?: undefined;
  rowDragHandleColumn?: undefined;
};

type RowReorderEnabledProps<ColKey extends string, ItemKeyValueType> = {
  onRowReorder: (
    activeId: ItemKeyValueType,
    overId: ItemKeyValueType,
    visibleItemKeys: ItemKeyValueType[],
  ) => void | Promise<void>;
  /**
   * Column key that renders the drag handle when row reordering is enabled.
   */
  rowDragHandleColumn: ColKey;
};

type ListProps<ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string> = {
  activeCols: ColKey[];
  colTitles: ColTitles<ColKey>;
  colConfig: ColConfig<ListItem, ItemKeyValueType, ColKey>;
  filters?: FilterItem[];
  filterItem?: (item: ListItem, filters: ActiveFilters) => boolean;
  onServerFilter?: (filters: ActiveFilters) => Promise<void>;
  filterSize?: keyof typeof sizes;
  headerControls?: ReactNode;
  items: ListItem[];
  itemKey: keyof ListItem;
  itemSelectable?: boolean;
  /**
   * Controls whether selectable rows use checkboxes (multi-select) or radio buttons (single-select).
   * Defaults to "checkbox". When "radio", the header select-all is hidden and only one item can be selected.
   */
  selectionType?: "checkbox" | "radio";
  initialSelectedItems?: ItemKeyValueType[];
  disabled?: boolean;
  actions?: ListAction<ListItem>[];
  noDataElement?: ReactNode;
  emptyStateRow?: ReactNode;
  renderActionBar?: (
    selectedItems: ItemKeyValueType[],
    clearSelection: () => void,
    selectionMode: SelectionMode,
    totalSelectable?: number,
  ) => ReactNode;
  containerClassName?: string;
  tableClassName?: string;
  paddingLeft?: Partial<Record<Breakpoint, string>>;
  paddingRight?: Partial<Record<Breakpoint, string>>;
  overflowContainer?: boolean;
  stickyBgColor?: string;
  total?: number;
  /**
   * Total number of disabled items across all pages.
   * Used with total to calculate selectable count: total - totalDisabled
   */
  totalDisabled?: number;
  itemName?: {
    singular: string;
    plural: string;
  };
  initialActiveFilters?: ActiveFilters;
  /**
   * When true, suppresses the built-in item count display below the filter bar.
   * Use when the parent component renders its own count (e.g., MinerList subtitle).
   */
  hideTotal?: boolean;
  /**
   * Optional callback to attach refs to list row elements.
   * Useful for viewport visibility tracking (Intersection Observer).
   * @param itemKey - The key value of the item
   * @param element - The tr element for the row (null on unmount)
   */
  itemRef?: (itemKey: ItemKeyValueType, element: HTMLTableRowElement | null) => void;
  /**
   * Whether server-side filters are currently active.
   * Used to determine selection mode: "all" (no filters) vs "subset" (with filters).
   */
  hasActiveFilters?: boolean;
  /**
   * Callback when filters change.
   * Called with the current active filters whenever the user modifies filters.
   */
  onFilterChange?: (filters: ActiveFilters) => void;
  /*
   * Optional callback for infinite scroll. Called when the user scrolls
   * near the bottom of the list.
   */
  onLoadMore?: () => void;
  /**
   * Whether more items are available to load. When false, onLoadMore
   * will not be triggered.
   */
  hasMore?: boolean;
  /**
   * Whether the list is currently loading more items.
   */
  isLoadingMore?: boolean;
  /**
   * Optional callback to determine if a specific row should be disabled.
   * Disabled rows are greyed out and cannot be selected.
   * @param item - The list item
   * @returns true if the row should be disabled
   */
  isRowDisabled?: (item: ListItem) => boolean;
  /**
   * Optional set of column keys that should NOT be affected by disabled row styling.
   * These columns will maintain full opacity even when the row is disabled.
   */
  columnsExemptFromDisabledStyling?: Set<ColKey>;
  /**
   * Set of column keys that support sorting.
   * When provided, these columns will have clickable headers with sort indicators.
   */
  sortableColumns?: Set<ColKey>;
  /**
   * Current sort state. When provided, shows sort indicator on the sorted column.
   */
  currentSort?: { field: ColKey; direction: SortDirection };
  /**
   * Callback fired when a sortable column header is clicked.
   * The direction passed is the NEW direction to sort by.
   */
  onSort?: (field: ColKey, direction: SortDirection) => void;
  stickyFirstColumn?: boolean;
  /**
   * Optional callback to determine the default sort direction for a column.
   * Called when clicking on a column that isn't currently sorted.
   * Defaults to "desc" if not provided.
   */
  getDefaultSortDirection?: (field: ColKey) => SortDirection;
  /**
   * Optional content to render at the bottom of the scroll container.
   * Useful for pagination controls that should scroll with the list content.
   */
  footerContent?: ReactNode;
  /**
   * When true, apply the configured column widths to the <th>/<td> cells
   * instead of the inner content wrapper. Use this when the configured width
   * should represent the total cell width including inner padding.
   */
  applyColumnWidthsToCells?: boolean;
  /**
   * When true, renders filters outside/above the scroll container so they remain
   * visible while scrolling. Default is false (filters scroll with content).
   */
  /**
   * Ref forwarded to the scrollable container element.
   * Useful for programmatic scroll control (e.g., scroll-to-top on pagination).
   */
  scrollRef?: Ref<HTMLDivElement>;
  /**
   * When true, skips automatic cleanup of customSelectedItems that are not
   * in the current items list. Use for paginated lists where the parent
   * manages selections across pages.
   */
  preserveOffPageSelection?: boolean;
  /**
   * When true, header and row checkbox behavior stays scoped to the current
   * page instead of promoting selection into a dataset-wide "all" state.
   */
  pageScopedSelection?: boolean;
  /**
   * Optional accessibility labels for column headers when the visible title is empty or abbreviated.
   */
  columnHeaderAriaLabels?: Partial<Record<ColKey, string>>;
  /**
   * Optional callback fired when a row is clicked.
   * When provided, rows get cursor-pointer and hover styling.
   *
   * Standard interactive descendants (button, a, input, select, textarea,
   * [role="button"], [role="link"]) are automatically excluded — clicks on
   * them will not trigger this callback. For non-standard interactive elements
   * (e.g. a clickable `<div>`), add the `data-interactive` attribute.
   */
  onRowClick?: (item: ListItem, index: number) => void;
} & (ControlledSelectionModeProps<ItemKeyValueType> | UncontrolledSelectionModeProps<ItemKeyValueType>) &
  (RowReorderDisabledProps | RowReorderEnabledProps<ColKey, ItemKeyValueType>);

type SortableDragHandleProps = Pick<ReturnType<typeof useSortable>, "attributes" | "listeners">;

type ListRowRenderProps<ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string> = {
  item: ListItem;
  index: number;
  itemKey: keyof ListItem;
  itemSelectable: boolean;
  selectionType: "checkbox" | "radio";
  radioGroupName: string;
  pageScopedSelection: boolean;
  currentSelectionMode: SelectionMode;
  currentSelectedItems: ItemKeyValueType[];
  activeCols: ColKey[];
  actions: ListAction<ListItem>[];
  rowDisabled: boolean;
  columnsExemptFromDisabledStyling?: Set<ColKey>;
  colConfig: ColConfig<ListItem, ItemKeyValueType, ColKey>;
  tdClassList: string;
  firstStickyClasses: string;
  secondStickyClasses: string;
  stickyFirstColumn: boolean;
  columnShadowBaseClassList: string;
  columnShadowVisibleClassList: string;
  stickyStateHorizontalIsStuck: boolean;
  applyColumnWidthsToCells: boolean;
  extendRowDividerToContainerEdge: boolean;
  rightPaddingClasses: string;
  paddingCssVariables: Record<string, string>;
  tdPaddingClassList: string;
  disabled: boolean;
  handleSelectItem: (
    itemKeyValue: ItemKeyValueType,
    checked: boolean,
    index: number,
    event: ChangeEvent<HTMLInputElement>,
  ) => void;
  itemRef?: (itemKey: ItemKeyValueType, element: HTMLTableRowElement | null) => void;
  rowDragHandleColumn?: ColKey;
  dragHandleProps?: SortableDragHandleProps;
  rowClassName?: string;
  rowStyle?: CSSProperties;
  rowRef?: (element: HTMLTableRowElement | null) => void;
  onRowClick?: (item: ListItem, index: number) => void;
};

const renderListRow = <ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string>({
  item,
  index,
  itemKey,
  itemSelectable,
  selectionType,
  radioGroupName,
  pageScopedSelection,
  currentSelectionMode,
  currentSelectedItems,
  activeCols,
  actions,
  rowDisabled,
  columnsExemptFromDisabledStyling,
  colConfig,
  tdClassList,
  firstStickyClasses,
  secondStickyClasses,
  stickyFirstColumn,
  columnShadowBaseClassList,
  columnShadowVisibleClassList,
  stickyStateHorizontalIsStuck,
  applyColumnWidthsToCells,
  extendRowDividerToContainerEdge,
  rightPaddingClasses,
  paddingCssVariables,
  tdPaddingClassList,
  disabled,
  handleSelectItem,
  itemRef,
  rowDragHandleColumn,
  dragHandleProps,
  rowClassName = rowClassList,
  rowStyle,
  rowRef,
  onRowClick,
}: ListRowRenderProps<ListItem, ItemKeyValueType, ColKey>) => {
  const visibleActions = actions.filter((action) => !resolveListActionValue(action.hidden, item));
  const singleVisibleAction = visibleActions.length === 1 ? visibleActions[0] : null;
  const singleVisibleActionTitle = singleVisibleAction
    ? resolveListActionValue(singleVisibleAction.title, item)
    : undefined;
  const singleVisibleActionDisabled = singleVisibleAction
    ? rowDisabled || resolveListActionValue(singleVisibleAction.disabled, item) === true
    : rowDisabled;
  const singleVisibleActionVariant =
    singleVisibleAction && resolveListActionValue(singleVisibleAction.variant, item) === "destructive"
      ? variants.secondaryDanger
      : variants.secondary;
  const rowKey = item[itemKey] as ItemKeyValueType;
  const isActionColumnVisible = actions.length > 0;
  const shouldExtendDividerForDataCell =
    extendRowDividerToContainerEdge && !isActionColumnVisible && activeCols.length > 0;

  return (
    <tr
      key={rowKey as string | number}
      className={clsx(rowClassName, onRowClick && "group cursor-pointer")}
      ref={(element) => {
        itemRef?.(rowKey, element);
        rowRef?.(element);
      }}
      data-testid="list-row"
      style={rowStyle}
      onClick={
        onRowClick
          ? (e: MouseEvent<HTMLTableRowElement>) => {
              const target = e.target;
              if (!(target instanceof Element)) return;
              if (!e.currentTarget.contains(target)) return;
              if (target.closest(INTERACTIVE_ELEMENT_SELECTOR)) return;
              if (target.closest("[data-no-row-click]")) return;
              onRowClick(item, index);
            }
          : undefined
      }
      onKeyDown={
        onRowClick
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                const target = e.target;
                if (!(target instanceof Element)) return;
                if (target.closest(INTERACTIVE_ELEMENT_SELECTOR)) return;
                e.preventDefault();
                onRowClick(item, index);
              }
            }
          : undefined
      }
      tabIndex={onRowClick ? 0 : undefined}
    >
      {itemSelectable && (
        <td
          className={clsx(tdClassList, firstStickyClasses, "w-9", onRowClick && rowHoverOverlayClassList)}
          style={paddingCssVariables}
          data-testid="checkbox"
          data-no-row-click
        >
          <div
            className={clsx("w-9 truncate overflow-hidden py-3", {
              "opacity-50": rowDisabled,
            })}
          >
            {selectionType === "radio" ? (
              <Radio
                name={radioGroupName}
                value={String(rowKey)}
                selected={currentSelectedItems.includes(rowKey)}
                onChange={(e) => handleSelectItem(rowKey, e.target.checked, index, e)}
                disabled={rowDisabled}
              />
            ) : (
              <Checkbox
                checked={
                  pageScopedSelection && currentSelectionMode === "all"
                    ? !rowDisabled
                    : currentSelectedItems.includes(rowKey)
                }
                onChange={(e) => handleSelectItem(rowKey, e.target.checked, index, e)}
                disabled={rowDisabled}
              />
            )}
          </div>
        </td>
      )}

      {activeCols.map((row, columnIndex) => {
        const isExempt = columnsExemptFromDisabledStyling?.has(row) ?? false;
        const columnWidthClass = colConfig[row]?.width;
        const allowWrap = colConfig[row]?.allowWrap ?? false;
        const content = colConfig[row]?.component
          ? colConfig[row].component(item, currentSelectedItems)
          : typeof item === "object" && item !== null && row in item
            ? ((item as Record<string, unknown>)[row as string] as ReactNode)
            : null;
        const isDragHandleColumn = rowDragHandleColumn === row && dragHandleProps !== undefined;

        return (
          <td
            className={clsx(
              tdClassList,
              onRowClick && rowHoverOverlayClassList,
              columnIndex === 0 && stickyFirstColumn && (itemSelectable ? secondStickyClasses : firstStickyClasses),
              columnIndex === 0 && stickyFirstColumn && columnShadowBaseClassList,
              columnIndex === 0 && stickyFirstColumn && stickyStateHorizontalIsStuck && columnShadowVisibleClassList,
              columnIndex === 0 && stickyFirstColumn && "border-r border-border-5 phone:border-r-0",
              columnIndex === activeCols.length - 1 && shouldExtendDividerForDataCell && "relative",
              applyColumnWidthsToCells && columnWidthClass,
              columnIndex === activeCols.length - 1 && rightPaddingClasses,
            )}
            key={row}
            style={paddingCssVariables}
            data-testid={row}
          >
            <div
              className={clsx(
                allowWrap ? "overflow-hidden" : "truncate overflow-hidden",
                columnIndex === 1 && stickyFirstColumn ? "py-3 pr-2 pl-4 phone:pl-2" : tdPaddingClassList,
                applyColumnWidthsToCells ? "box-border w-full" : columnWidthClass,
                {
                  "opacity-50": rowDisabled && !isExempt,
                },
                {
                  "text-core-primary-50": disabled,
                },
              )}
            >
              {isDragHandleColumn ? (
                <div
                  {...dragHandleProps.attributes}
                  {...dragHandleProps.listeners}
                  role="button"
                  aria-label="Drag to reorder row"
                  tabIndex={0}
                  className="cursor-grab touch-none active:cursor-grabbing"
                  data-testid="reorder-handle"
                >
                  {content}
                </div>
              ) : (
                content
              )}
            </div>
            {columnIndex === activeCols.length - 1 && shouldExtendDividerForDataCell ? (
              <div aria-hidden className={rowDividerExtensionClassList} data-testid="row-divider-extension" />
            ) : null}
          </td>
        );
      })}

      {visibleActions.length === 1 && singleVisibleAction && singleVisibleActionTitle ? (
        <td
          className={clsx(tdClassList, {
            "opacity-50": rowDisabled,
            relative: extendRowDividerToContainerEdge,
          })}
          data-testid="action"
          data-no-row-click
        >
          <div className={clsx("flex justify-end", tdPaddingClassList)}>
            <Button
              variant={singleVisibleActionVariant}
              size={sizes.compact}
              text={singleVisibleActionTitle}
              onClick={() => singleVisibleAction.actionHandler(item)}
              disabled={singleVisibleActionDisabled}
            />
          </div>
          {extendRowDividerToContainerEdge ? (
            <div aria-hidden className={rowDividerExtensionClassList} data-testid="row-divider-extension" />
          ) : null}
        </td>
      ) : visibleActions.length > 1 ? (
        <td
          className={clsx(tdClassList, {
            "opacity-50": rowDisabled,
            relative: extendRowDividerToContainerEdge,
          })}
          data-testid="action"
          data-no-row-click
        >
          <div className={clsx("w-11", tdPaddingClassList)}>
            <PopoverProvider>
              <ListActions<ListItem> item={item} actions={visibleActions} disabled={rowDisabled} />
            </PopoverProvider>
          </div>
          {extendRowDividerToContainerEdge ? (
            <div aria-hidden className={rowDividerExtensionClassList} data-testid="row-divider-extension" />
          ) : null}
        </td>
      ) : actions.length > 0 ? (
        <td
          className={clsx(tdClassList, {
            "opacity-50": rowDisabled,
            relative: extendRowDividerToContainerEdge,
          })}
          data-testid="action"
          data-no-row-click
        >
          <div className={clsx("w-11", tdPaddingClassList)} />
          {extendRowDividerToContainerEdge ? (
            <div aria-hidden className={rowDividerExtensionClassList} data-testid="row-divider-extension" />
          ) : null}
        </td>
      ) : null}
    </tr>
  );
};

type StaticListRowProps<ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string> = Omit<
  ListRowRenderProps<ListItem, ItemKeyValueType, ColKey>,
  "rowRef" | "rowStyle" | "rowClassName" | "dragHandleProps"
>;

const StaticListRow = <ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string>(
  props: StaticListRowProps<ListItem, ItemKeyValueType, ColKey>,
) => renderListRow(props);

type SortableListRowProps<
  ListItem,
  ItemKeyValueType,
  ColKey extends string = keyof ListItem & string,
> = StaticListRowProps<ListItem, ItemKeyValueType, ColKey> & {
  dragId: UniqueIdentifier;
};

const SortableListRow = <ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string>({
  dragId,
  ...props
}: SortableListRowProps<ListItem, ItemKeyValueType, ColKey>) => {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useSortable({
    id: dragId,
    animateLayoutChanges: () => false,
  });

  return renderListRow({
    ...props,
    dragHandleProps: { attributes, listeners },
    rowRef: setNodeRef,
    rowStyle: {
      transform: CSS.Translate.toString(transform),
      transition: undefined,
      opacity: isDragging ? 0.5 : 1,
      position: "relative",
      zIndex: isDragging ? 1 : 0,
    },
  });
};

const cellClassList = "text-left";
const rowClassList = "border-b border-border-5";
const rowHoverOverlayClassList =
  "group-hover:bg-[linear-gradient(var(--color-surface-5),var(--color-surface-5))] dark:group-hover:bg-[linear-gradient(var(--color-core-primary-5),var(--color-core-primary-5))]";
const rowDividerExtensionClassList = `pointer-events-none absolute top-0 left-full h-full w-[100vw] border-b border-border-5 bg-transparent ${rowHoverOverlayClassList}`;
const thClassList = cellClassList + " py-3 text-emphasis-300 text-text-primary";
const baseStickyClassList = "tablet:sticky laptop:sticky desktop:sticky z-1";
const tdClassList = "text-left text-300";
const tdPaddingClassList = "px-2 py-3";
const stickyShadowMaskColors: Record<string, string> = {
  "bg-surface-base": "var(--color-surface-base)",
  "bg-surface-elevated-base": "var(--color-surface-elevated-base)",
  "bg-surface-5": "var(--color-surface-5)",
};
// use after element for shadow (hidden on phone since column isn't sticky)
// use before element as an opaque mask and after element as the shadow so
// horizontally scrolled content cannot bleed through at the sticky edge
const columnShadowBaseClassList =
  "before:content-[''] before:absolute before:top-0 before:right-[-6px] before:bottom-[-1px] before:w-[9px] before:bg-[var(--list-sticky-shadow-mask-bg)] before:opacity-0 before:transition-opacity before:duration-500 group-hover:before:bg-[linear-gradient(var(--color-surface-5),var(--color-surface-5))] dark:group-hover:before:bg-[linear-gradient(var(--color-core-primary-5),var(--color-core-primary-5))] after:content-[''] after:absolute after:top-0 after:right-[-6px] after:bottom-[-1px] after:w-[9px] after:bg-[linear-gradient(90deg,rgba(0,0,0,0.06)0%,rgba(0,0,0,0)100%)] after:opacity-0 after:transition-opacity after:duration-500 phone:before:content-none phone:after:content-none";
const columnShadowVisibleClassList = "before:opacity-100 after:opacity-100";

const List = <ListItem, ItemKeyValueType, ColKey extends string = keyof ListItem & string>({
  activeCols,
  colTitles,
  colConfig,
  filters,
  filterItem,
  onServerFilter,
  filterSize = sizes.compact,
  headerControls,
  initialSelectedItems = [],
  customSetSelectedItems,
  customSelectedItems,
  items,
  itemKey,
  itemSelectable = false,
  selectionType = "checkbox",
  disabled = false,
  actions = [],
  noDataElement,
  emptyStateRow,
  initialActiveFilters,
  hideTotal = false,
  renderActionBar,
  containerClassName = "",
  tableClassName,
  paddingLeft,
  paddingRight,
  overflowContainer = true,
  stickyBgColor = "bg-surface-base",
  total,
  totalDisabled = 0,
  itemName = { singular: "item", plural: "items" },
  itemRef,
  hasActiveFilters = false,
  onFilterChange,
  onSelectionModeChange,
  customSelectionMode,
  onLoadMore,
  hasMore = false,
  isLoadingMore = false,
  isRowDisabled,
  columnsExemptFromDisabledStyling,
  sortableColumns,
  currentSort,
  onSort,
  onRowReorder,
  rowDragHandleColumn,
  stickyFirstColumn = true,
  getDefaultSortDirection,
  footerContent,
  applyColumnWidthsToCells = false,
  scrollRef,
  preserveOffPageSelection = false,
  pageScopedSelection = false,
  columnHeaderAriaLabels,
  onRowClick,
}: ListProps<ListItem, ItemKeyValueType, ColKey>) => {
  const { refs, stickyState } = useStickyState();
  const loadMoreTriggerRef = useRef<HTMLDivElement>(null);
  const internalScrollRef = useRef<HTMLDivElement>(null);
  const tableRef = useRef<HTMLTableElement>(null);
  const lastClickedIndexRef = useRef<number | null>(null);

  const radioGroupName = useId();
  const [selectedItems, setSelectedItems] = useState<ItemKeyValueType[]>(initialSelectedItems);
  const [filteredItems, setFilteredItems] = useState<ListItem[]>(items);
  const [selectionMode, setSelectionMode] = useState<SelectionMode>("none");
  const [hoveredHeader, setHoveredHeader] = useState<ColKey | null>(null);
  const [hasHorizontalOverflow, setHasHorizontalOverflow] = useState(false);
  const isServerSideFiltering = useMemo(() => onServerFilter !== undefined, [onServerFilter]);
  const prevCustomSelectedLengthRef = useRef<number | undefined>(undefined);
  const currentSelectionMode = customSelectionMode ?? selectionMode;
  const currentSelectedItems = customSelectedItems ?? selectedItems;
  const rowDragEnabled = !!onRowReorder && filteredItems.length > 1;
  const activeColsKey = useMemo(() => activeCols.join("|"), [activeCols]);
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 8 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  // Helper to get selectable items (excludes disabled rows)
  const getSelectableItems = useCallback(
    (itemList: ListItem[]) => {
      if (!isRowDisabled) return itemList;
      return itemList.filter((item) => !isRowDisabled(item));
    },
    [isRowDisabled],
  );

  // Calculate total selectable count (total - disabled)
  const totalSelectable = useMemo(() => {
    if (total === undefined) return undefined;
    return total - totalDisabled;
  }, [total, totalDisabled]);

  // Memoized callback for action bar - defined first so handleSelectAll can use it
  const clearSelection = useCallback(() => {
    customSetSelectedItems ? customSetSelectedItems([]) : setSelectedItems([]);
    setSelectionMode("none");
    onSelectionModeChange?.("none");
  }, [customSetSelectedItems, onSelectionModeChange]);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      // Select only filtered items (respects both client-side and server-side filters)
      const selectableItems = getSelectableItems(filteredItems);
      const selection = selectableItems.map((item) => item[itemKey] as ItemKeyValueType);
      if (selection.length === 0) {
        clearSelection();
        return;
      }
      customSetSelectedItems ? customSetSelectedItems(selection) : setSelectedItems(selection);
      // If we're selecting filtered items, it's a subset (unless all items match the filter)
      const allItemsMatchFilter = filteredItems.length === items.length;
      const newMode = pageScopedSelection || hasActiveFilters || !allItemsMatchFilter ? "subset" : "all";
      setSelectionMode(newMode);
      onSelectionModeChange?.(newMode);
    } else {
      clearSelection();
    }
  };

  // Clear selection anchor when bulk selection changes (Select All or Clear Selection)
  useEffect(() => {
    if (currentSelectionMode === "all" || currentSelectionMode === "none") {
      lastClickedIndexRef.current = null;
    }
  }, [currentSelectionMode]);

  // Reset selectionMode when customSelectedItems is externally changed from non-empty to empty
  // This handles "Select none" from external controls like ModalSelectAllFooter
  useEffect(() => {
    const prevLength = prevCustomSelectedLengthRef.current;
    const currentLength = customSelectedItems?.length;

    // Only reset when selection changed from non-empty to empty
    if (prevLength !== undefined && prevLength > 0 && currentLength === 0 && currentSelectionMode !== "none") {
      setSelectionMode("none");
      onSelectionModeChange?.("none");
    }

    // Update ref for next render
    prevCustomSelectedLengthRef.current = currentLength;
  }, [customSelectedItems?.length, currentSelectionMode, onSelectionModeChange]);

  const selectRange = (anchorIndex: number, targetIndex: number, currentSelected: ItemKeyValueType[]) => {
    const start = Math.min(anchorIndex, targetIndex);
    const end = Math.max(anchorIndex, targetIndex);
    const rangeItems = filteredItems.slice(start, end + 1);
    const selectableRangeItems = getSelectableItems(rangeItems);
    const rangeKeys = selectableRangeItems.map((item) => item[itemKey] as ItemKeyValueType);

    const selectedSet = new Set(currentSelected);
    rangeKeys.forEach((key) => selectedSet.add(key));
    return Array.from(selectedSet);
  };

  const toggleSingleItem = (itemKeyValue: ItemKeyValueType, checked: boolean, currentSelected: ItemKeyValueType[]) => {
    if (checked && !currentSelected.includes(itemKeyValue)) {
      return [...currentSelected, itemKeyValue];
    } else if (!checked) {
      return currentSelected.filter((key) => key !== itemKeyValue);
    }
    return currentSelected;
  };

  const handleSelectItem = (
    itemKeyValue: ItemKeyValueType,
    checked: boolean,
    index: number,
    event: ChangeEvent<HTMLInputElement>,
  ) => {
    const currentSelected =
      pageScopedSelection && currentSelectionMode === "all"
        ? getSelectableItems(filteredItems).map((item) => item[itemKey] as ItemKeyValueType)
        : currentSelectedItems;
    const isShiftClick = event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey;
    const canRangeSelect = isShiftClick && lastClickedIndexRef.current !== null && checked;

    let newSelectedItems: ItemKeyValueType[];

    if (selectionType === "radio") {
      // Radio mode: always single-select, no toggle-off.
      // `checked` is always true for radio onChange events — we ignore it intentionally.
      newSelectedItems = [itemKeyValue];
    } else if (canRangeSelect) {
      newSelectedItems = selectRange(lastClickedIndexRef.current!, index, currentSelected);
      lastClickedIndexRef.current = null;
    } else {
      newSelectedItems = toggleSingleItem(itemKeyValue, checked, currentSelected);
      lastClickedIndexRef.current = checked ? index : null;
    }

    if (customSetSelectedItems) {
      customSetSelectedItems(newSelectedItems);
    } else {
      setSelectedItems(newSelectedItems);
    }

    const selectableItems = getSelectableItems(items);
    const allItemsSelected = newSelectedItems.length === selectableItems.length && selectableItems.length > 0;
    const newMode =
      newSelectedItems.length === 0
        ? "none"
        : pageScopedSelection
          ? "subset"
          : allItemsSelected && !hasActiveFilters
            ? "all"
            : "subset";
    setSelectionMode(newMode);
    onSelectionModeChange?.(newMode);
  };

  const allSelected = useMemo(() => {
    // Check if all filtered items are selected (not all items)
    const selectableItems = getSelectableItems(filteredItems);
    const selectableCount = selectableItems.length;
    if (selectableCount === 0) return false;
    if (pageScopedSelection && currentSelectionMode === "all") {
      return selectableCount > 0;
    }

    const currentSelected = currentSelectedItems;
    // Use Set for O(1) lookups instead of O(n) array.includes()
    const selectedSet = new Set<ItemKeyValueType>(currentSelected);
    return selectableItems.every((item) => selectedSet.has(item[itemKey] as ItemKeyValueType));
  }, [currentSelectedItems, currentSelectionMode, filteredItems, getSelectableItems, itemKey, pageScopedSelection]);

  const visibleSelectedCount = useMemo(() => {
    const selectableItems = getSelectableItems(filteredItems);
    if (selectableItems.length === 0) return 0;

    const selectedSet = new Set<ItemKeyValueType>(currentSelectedItems);
    return selectableItems.filter((item) => selectedSet.has(item[itemKey] as ItemKeyValueType)).length;
  }, [currentSelectedItems, filteredItems, getSelectableItems, itemKey]);

  const handleServerFiltering = useCallback(
    (activeFilters: ActiveFilters) => {
      if (isServerSideFiltering) {
        onServerFilter!(activeFilters);
      }
      onFilterChange?.(activeFilters);
    },
    [isServerSideFiltering, onServerFilter, onFilterChange],
  );

  const handleClientFiltering = useCallback(
    (activeFilters: ActiveFilters) => {
      setFilteredItems(items.filter((item) => filterItem === undefined || filterItem(item, activeFilters)));
      onFilterChange?.(activeFilters);
    },
    [filterItem, items, onFilterChange],
  );

  // Determine if Filters component will render (and handle filtering)
  const shouldRenderFilters = !!(filters?.length || headerControls);

  // Update filteredItems when items change
  useEffect(() => {
    if (isServerSideFiltering) {
      // Server-side filtering: items are already filtered by server
      setFilteredItems(items);
    } else if (!shouldRenderFilters && filterItem) {
      // Client-side filtering without Filters component: apply filterItem directly
      setFilteredItems(items.filter((item) => filterItem(item, { buttonFilters: [], dropdownFilters: {} })));
    } else if (!shouldRenderFilters) {
      // No filtering at all: use items directly
      setFilteredItems(items);
    }
    // When shouldRenderFilters is true, Filters component handles filtering via onFilter callback
  }, [items, isServerSideFiltering, shouldRenderFilters, filterItem]);

  // Clear selection anchor when filtered items change to prevent invalid range selection
  useEffect(() => {
    lastClickedIndexRef.current = null;
  }, [filteredItems]);

  // Sync selected items when items list changes
  useEffect(() => {
    const selectableItems = getSelectableItems(items);
    const currentItemKeys = new Set(items.map((item) => item[itemKey] as ItemKeyValueType));
    const currentSelected = currentSelectedItems;

    // In "all" mode, ensure all selectable current items are selected (handles Load More)
    if (currentSelectionMode === "all") {
      const allSelectableItemKeys = selectableItems.map((item) => item[itemKey] as ItemKeyValueType);
      const currentSelectedSet = new Set(currentSelected);
      const needsUpdate = allSelectableItemKeys.some((key) => !currentSelectedSet.has(key));

      if (needsUpdate) {
        if (customSetSelectedItems) {
          customSetSelectedItems(allSelectableItemKeys);
        } else {
          setSelectedItems(allSelectableItemKeys);
        }
      }
      return;
    }

    // When preserveOffPageSelection is enabled, skip cleanup so the parent
    // can manage selections across pages.
    if (preserveOffPageSelection) {
      return;
    }

    if (customSetSelectedItems && customSelectedItems) {
      const newSelectedItems = customSelectedItems.filter((selectedKey) => currentItemKeys.has(selectedKey));
      if (newSelectedItems.length !== customSelectedItems.length) {
        customSetSelectedItems(newSelectedItems);
        const newMode = newSelectedItems.length === 0 ? "none" : "subset";
        setSelectionMode(newMode);
        onSelectionModeChange?.(newMode);
      }
    } else {
      const newSelectedItems = selectedItems.filter((selectedKey) => currentItemKeys.has(selectedKey));
      if (newSelectedItems.length !== selectedItems.length) {
        setSelectedItems(newSelectedItems);
        const newMode = newSelectedItems.length === 0 ? "none" : "subset";
        setSelectionMode(newMode);
        onSelectionModeChange?.(newMode);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    items,
    itemKey,
    customSetSelectedItems,
    onSelectionModeChange,
    isRowDisabled,
    currentSelectedItems,
    currentSelectionMode,
  ]);

  // Infinite scroll: trigger loadMore when scroll reaches near bottom
  useEffect(() => {
    if (!onLoadMore || !hasMore || isLoadingMore) return;

    const trigger = loadMoreTriggerRef.current;
    if (!trigger) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const [entry] = entries;
        if (entry.isIntersecting && hasMore && !isLoadingMore) {
          onLoadMore();
        }
      },
      {
        rootMargin: "200px", // Start loading 200px before reaching the bottom
        threshold: 0,
      },
    );

    observer.observe(trigger);

    return () => {
      observer.disconnect();
    };
  }, [onLoadMore, hasMore, isLoadingMore]);

  const paddingCssVariables = useMemo(() => {
    const style: Record<string, string> = {};
    Object.entries(breakpoints).forEach(([, breakpoint]) => {
      style[`--list-padding-${breakpoint}`] = paddingLeft?.[breakpoint] || "0px";
      style[`--list-padding-right-${breakpoint}`] = paddingRight?.[breakpoint] || "0px";
    });
    style["--list-sticky-shadow-mask-bg"] = stickyShadowMaskColors[stickyBgColor] ?? "var(--color-surface-base)";
    return style;
  }, [paddingLeft, paddingRight, stickyBgColor]);

  const paddingClasses = clsx(
    paddingLeft
      ? [
          "phone:pl-(--list-padding-phone)",
          "tablet:pl-(--list-padding-tablet)",
          "laptop:pl-(--list-padding-laptop)",
          "desktop:pl-(--list-padding-desktop)",
        ]
      : "",
  );

  const rightPaddingClasses = clsx(
    (!overflowContainer || hasHorizontalOverflow) && paddingRight
      ? [
          "phone:pr-(--list-padding-right-phone)",
          "tablet:pr-(--list-padding-right-tablet)",
          "laptop:pr-(--list-padding-right-laptop)",
          "desktop:pr-(--list-padding-right-desktop)",
        ]
      : "",
  );

  const setCombinedScrollRef = useCallback(
    (node: HTMLDivElement | null) => {
      internalScrollRef.current = node;

      if (!scrollRef) return;

      if (typeof scrollRef === "function") {
        scrollRef(node);
        return;
      }

      (scrollRef as MutableRefObject<HTMLDivElement | null>).current = node;
    },
    [scrollRef],
  );

  useEffect(() => {
    if (!overflowContainer) {
      setHasHorizontalOverflow(false);
      return;
    }

    const scrollContainer = internalScrollRef.current;
    const table = tableRef.current;
    if (!scrollContainer || !table) return;

    const updateOverflowState = () => {
      const measuredTableWidth = getOverflowMeasurementWidth(table, paddingRight, hasHorizontalOverflow);
      setHasHorizontalOverflow(measuredTableWidth > scrollContainer.clientWidth + 1);
    };

    updateOverflowState();
    window.addEventListener("resize", updateOverflowState);

    const resizeObserver =
      typeof ResizeObserver !== "undefined" ? new ResizeObserver(() => updateOverflowState()) : undefined;
    resizeObserver?.observe(scrollContainer);
    resizeObserver?.observe(table);

    return () => {
      window.removeEventListener("resize", updateOverflowState);
      resizeObserver?.disconnect();
    };
  }, [
    activeColsKey,
    filteredItems.length,
    itemSelectable,
    actions.length,
    hasHorizontalOverflow,
    overflowContainer,
    paddingRight,
    tableClassName,
  ]);

  const firstStickyClasses = clsx(
    baseStickyClassList,
    "tablet:left-0 laptop:left-0 desktop:left-0",
    stickyBgColor,
    paddingClasses,
  );

  const secondStickyClasses = clsx(
    baseStickyClassList,
    stickyBgColor,
    "desktop:left-[calc(var(--list-padding-desktop)+theme(spacing.9))]",
    "laptop:left-[calc(var(--list-padding-laptop)+theme(spacing.9))]",
    "tablet:left-[calc(var(--list-padding-tablet)+theme(spacing.9))]",
  );

  const filtersElement = (filters?.length || headerControls) && (
    <Filters<ListItem>
      key={JSON.stringify(initialActiveFilters ?? null)}
      className={clsx("gap-4 py-6", paddingClasses)}
      filterItems={filters ?? []}
      filterSize={filterSize}
      items={items}
      onFilter={isServerSideFiltering ? handleServerFiltering : handleClientFiltering}
      isServerSide={isServerSideFiltering}
      headerControls={headerControls}
      initialActiveFilters={initialActiveFilters}
    />
  );

  const visibleItemKeys = useMemo(
    () => filteredItems.map((item) => item[itemKey] as ItemKeyValueType),
    [filteredItems, itemKey],
  );

  const handleRowDragEnd = useCallback(
    (event: DragEndEvent) => {
      if (!onRowReorder || !event.over || event.active.id === event.over.id) {
        return;
      }

      // Async handlers surface their own UI; catch here to avoid unhandled rejections.
      void Promise.resolve(
        onRowReorder(event.active.id as ItemKeyValueType, event.over.id as ItemKeyValueType, visibleItemKeys),
      ).catch((error) => {
        console.error("List row reorder failed:", error);
      });
    },
    [onRowReorder, visibleItemKeys],
  );

  const totalColumnCount = activeCols.length + (itemSelectable ? 1 : 0) + (actions.length > 0 ? 1 : 0);
  const renderRow = (item: ListItem, index: number) => {
    const rowKey = item[itemKey] as ItemKeyValueType;
    const sharedRowProps: StaticListRowProps<ListItem, ItemKeyValueType, ColKey> = {
      item,
      index,
      itemKey,
      itemSelectable,
      selectionType,
      radioGroupName,
      pageScopedSelection,
      currentSelectionMode,
      currentSelectedItems,
      activeCols,
      actions,
      rowDisabled: isRowDisabled?.(item) ?? false,
      columnsExemptFromDisabledStyling,
      colConfig,
      tdClassList,
      firstStickyClasses,
      secondStickyClasses,
      stickyFirstColumn,
      columnShadowBaseClassList,
      columnShadowVisibleClassList,
      stickyStateHorizontalIsStuck: stickyState.horizontal.isStuck,
      applyColumnWidthsToCells,
      extendRowDividerToContainerEdge: overflowContainer && !hasHorizontalOverflow,
      rightPaddingClasses,
      paddingCssVariables,
      tdPaddingClassList,
      disabled,
      handleSelectItem,
      itemRef,
      onRowClick,
    };

    if (rowDragEnabled) {
      return (
        <SortableListRow<ListItem, ItemKeyValueType, ColKey>
          key={rowKey as string | number}
          dragId={rowKey as UniqueIdentifier}
          {...sharedRowProps}
          rowDragHandleColumn={rowDragHandleColumn}
        />
      );
    }

    return <StaticListRow<ListItem, ItemKeyValueType, ColKey> key={rowKey as string | number} {...sharedRowProps} />;
  };

  const listContent = (
    <>
      <div ref={refs.vertical.start} />
      <div className="sticky top-0 flex justify-between">
        <div ref={refs.horizontal.start} />
        <div ref={refs.horizontal.end} />
      </div>
      <table ref={tableRef} className={clsx("min-w-full table-fixed border-collapse", tableClassName ?? "mb-6")}>
        <thead data-testid="list-header">
          <tr
            className={clsx(
              "sticky top-0 z-2 border-b border-border-5 transition-shadow duration-500",
              stickyBgColor,
              stickyState.vertical.isStuck
                ? "shadow-[0_4px_6px_0_rgba(0,0,0,0.06)]"
                : "shadow-[0_4px_6px_0_rgba(0,0,0,0)]",
            )}
          >
            {itemSelectable && (
              <th scope="col" className={clsx(thClassList, firstStickyClasses, "w-9")} style={paddingCssVariables}>
                {selectionType !== "radio" && (
                  <div className="w-9 truncate overflow-hidden" data-testid="select-all-checkbox">
                    <Checkbox
                      checked={allSelected}
                      partiallyChecked={visibleSelectedCount > 0 && !allSelected}
                      onChange={(e) => handleSelectAll(e.target.checked)}
                    />
                  </div>
                )}
              </th>
            )}

            {activeCols.map((row, idx) => {
              const isSortable = sortableColumns?.has(row);
              const isCurrentSort = currentSort?.field === row;
              const sortDirection = isCurrentSort ? currentSort.direction : undefined;
              const isHovering = hoveredHeader === row;
              const columnWidthClass = colConfig[row]?.width;

              const handleHeaderClick = () => {
                if (!isSortable || !onSort) return;

                let newDirection: SortDirection;
                if (isCurrentSort) {
                  newDirection = sortDirection === SORT_ASC ? SORT_DESC : SORT_ASC;
                } else {
                  newDirection = getDefaultSortDirection?.(row) ?? SORT_ASC;
                }
                onSort(row, newDirection);
              };

              return (
                <th
                  scope="col"
                  className={clsx(
                    idx === 1 && stickyFirstColumn ? "pl-4 phone:pl-2" : "pl-2",
                    thClassList,
                    idx === 0 && stickyFirstColumn && (itemSelectable ? secondStickyClasses : firstStickyClasses),
                    idx === 0 && stickyFirstColumn && columnShadowBaseClassList,
                    idx === 0 && stickyFirstColumn && stickyState.horizontal.isStuck && columnShadowVisibleClassList,
                    idx === 0 && stickyFirstColumn && "border-r border-border-5 phone:border-r-0",
                    idx === activeCols.length - 1 && rightPaddingClasses,
                    applyColumnWidthsToCells && columnWidthClass,
                  )}
                  key={idx}
                  style={paddingCssVariables}
                  aria-label={columnHeaderAriaLabels?.[row]}
                  aria-sort={isCurrentSort ? (sortDirection === SORT_ASC ? "ascending" : "descending") : undefined}
                >
                  {isSortable ? (
                    <button
                      type="button"
                      className={clsx(
                        "inline-flex w-full cursor-pointer items-center truncate overflow-hidden text-left select-none",
                        applyColumnWidthsToCells ? "box-border" : columnWidthClass,
                      )}
                      onClick={handleHeaderClick}
                      onMouseEnter={() => setHoveredHeader(row)}
                      onMouseLeave={() => setHoveredHeader(null)}
                    >
                      {colTitles[row]}
                      <SortIndicator
                        direction={sortDirection}
                        defaultDirection={getDefaultSortDirection?.(row)}
                        isHovering={isHovering}
                      />
                    </button>
                  ) : (
                    <div
                      className={clsx(
                        "inline-flex w-full items-center truncate overflow-hidden",
                        applyColumnWidthsToCells ? "box-border" : columnWidthClass,
                      )}
                    >
                      {colTitles[row]}
                    </div>
                  )}
                </th>
              );
            })}
            {actions.length > 0 && (
              <th scope="col" className={thClassList}>
                <div className="w-11 truncate overflow-hidden" />
              </th>
            )}
          </tr>
        </thead>
        {rowDragEnabled ? (
          <SortableContext items={visibleItemKeys as UniqueIdentifier[]} strategy={verticalListSortingStrategy}>
            <tbody data-testid="list-body">
              {filteredItems.length > 0
                ? filteredItems.map(renderRow)
                : emptyStateRow && (
                    <tr data-testid="list-empty-row">
                      <td colSpan={totalColumnCount}>{emptyStateRow}</td>
                    </tr>
                  )}
            </tbody>
          </SortableContext>
        ) : (
          <tbody data-testid="list-body">
            {filteredItems.length > 0
              ? filteredItems.map(renderRow)
              : emptyStateRow && (
                  <tr data-testid="list-empty-row">
                    <td colSpan={totalColumnCount}>{emptyStateRow}</td>
                  </tr>
                )}
          </tbody>
        )}
      </table>
      {footerContent}
      {onLoadMore && hasMore && (
        <div ref={loadMoreTriggerRef} className="flex justify-center py-6">
          {isLoadingMore && <ProgressCircular indeterminate />}
        </div>
      )}
      <div ref={refs.vertical.end} />
    </>
  );

  return (
    <>
      <div style={paddingCssVariables} className="sticky left-0 z-3">
        {filtersElement}
      </div>
      <div style={paddingCssVariables}>
        {!hideTotal && total !== undefined && (
          <div className="sticky left-0 flex">
            <div className={clsx("sticky left-0 pb-4 text-emphasis-300 text-text-primary-70", paddingClasses)}>
              {total} {total === 1 ? itemName.singular : itemName.plural}
            </div>
          </div>
        )}
        <div className={clsx("flex flex-col", containerClassName)}>
          <div
            ref={setCombinedScrollRef}
            className={clsx({
              "overflow-x-auto": overflowContainer && hasHorizontalOverflow,
              "overflow-x-hidden": overflowContainer && !hasHorizontalOverflow,
            })}
          >
            {!noDataElement || (items && items.length > 0) ? (
              rowDragEnabled ? (
                <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleRowDragEnd}>
                  {listContent}
                </DndContext>
              ) : (
                listContent
              )
            ) : (
              noDataElement
            )}
          </div>
          {renderActionBar && (
            <div className="w-full">
              {renderActionBar(currentSelectedItems, clearSelection, currentSelectionMode, totalSelectable)}
            </div>
          )}
        </div>
      </div>
    </>
  );
};

export default List;
export type { SelectionMode };
