export type SelectionMode = "single" | "range";

export type WithInputs = "date" | "date-and-time" | false;

export type Timeframe = "past" | "present" | "future";

export type WeekDay = 0 | 1 | 2 | 3 | 4 | 5 | 6;

export type DatePickerPopoverRenderMode = "inline" | "portal-fixed" | "portal-scrolling";
export type DatePickerLabelPlacement = "above" | "floating";

export type DaySelection = "none" | "single" | "range-first" | "range-middle" | "range-last";

export interface DayData {
  date: number;
  month: number;
  year: number;
  isCurrentMonth: boolean;
  today: boolean;
  disabled: boolean;
  dateObj: Date;
}

export interface DatePickerProps {
  id?: string;
  label?: string;
  floatingLabel?: boolean;
  onBlur?: () => void;
  onOpenChange?: (open: boolean) => void;
  selectionMode?: SelectionMode;
  withInputs?: WithInputs;
  displayMenu?: boolean;
  timeframe?: Timeframe;
  weekStartsOn?: WeekDay;
  isDateDisabled?: (date: Date) => boolean;
  selectedDate?: Date;
  selectedStartDate?: Date;
  selectedEndDate?: Date;
  onSelectedDateChange?: (date: Date) => void;
  onSelectedDateRangeChange?: (start: Date, end: Date) => void;
  presets?: Array<{ label: string; startDate: Date; endDate: Date }>;
  className?: string;
  testId?: string;
  disabled?: boolean;
  error?: boolean | string;
  popoverRenderMode?: DatePickerPopoverRenderMode;
}
