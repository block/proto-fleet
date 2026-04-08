import { useState } from "react";

import DatePicker from ".";

export const Single = () => {
  const [date, setDate] = useState<Date | undefined>();
  return (
    <div className="p-8">
      <DatePicker selectionMode="single" selectedDate={date} onSelectedDateChange={setDate} testId="single-picker" />
      {date && <p className="mt-4 text-300 text-text-primary">Selected: {date.toLocaleDateString()}</p>}
    </div>
  );
};

export const Range = () => {
  const [start, setStart] = useState<Date | undefined>();
  const [end, setEnd] = useState<Date | undefined>();
  return (
    <div className="p-8">
      <DatePicker
        selectionMode="range"
        selectedStartDate={start}
        selectedEndDate={end}
        onSelectedDateRangeChange={(s, e) => {
          setStart(s);
          setEnd(e);
        }}
        testId="range-picker"
      />
      {start && end && (
        <p className="mt-4 text-300 text-text-primary">
          Range: {start.toLocaleDateString()} — {end.toLocaleDateString()}
        </p>
      )}
    </div>
  );
};

export const WithPresets = () => {
  const [start, setStart] = useState<Date | undefined>();
  const [end, setEnd] = useState<Date | undefined>();
  return (
    <div className="p-8">
      <DatePicker
        selectionMode="range"
        displayMenu
        selectedStartDate={start}
        selectedEndDate={end}
        onSelectedDateRangeChange={(s, e) => {
          setStart(s);
          setEnd(e);
        }}
        testId="preset-picker"
      />
    </div>
  );
};

export const WithInputs = () => {
  const [date, setDate] = useState<Date | undefined>();
  return (
    <div className="p-8">
      <DatePicker
        selectionMode="single"
        withInputs="date"
        selectedDate={date}
        onSelectedDateChange={setDate}
        testId="input-picker"
      />
    </div>
  );
};

export const WithDateAndTimeInputs = () => {
  const [start, setStart] = useState<Date | undefined>();
  const [end, setEnd] = useState<Date | undefined>();
  return (
    <div className="p-8">
      <DatePicker
        selectionMode="range"
        withInputs="date-and-time"
        displayMenu
        selectedStartDate={start}
        selectedEndDate={end}
        onSelectedDateRangeChange={(s, e) => {
          setStart(s);
          setEnd(e);
        }}
        testId="datetime-picker"
      />
    </div>
  );
};

export const DisabledDates = () => {
  const [date, setDate] = useState<Date | undefined>();
  const isWeekend = (d: Date) => d.getDay() === 0 || d.getDay() === 6;
  return (
    <div className="p-8">
      <DatePicker
        selectionMode="single"
        selectedDate={date}
        onSelectedDateChange={setDate}
        isDateDisabled={isWeekend}
        testId="disabled-dates-picker"
      />
      <p className="mt-2 text-200 text-text-primary-50">Weekends are disabled</p>
    </div>
  );
};

export const PastOnly = () => {
  const [start, setStart] = useState<Date | undefined>();
  const [end, setEnd] = useState<Date | undefined>();
  return (
    <div className="p-8">
      <DatePicker
        selectionMode="range"
        displayMenu
        timeframe="past"
        selectedStartDate={start}
        selectedEndDate={end}
        onSelectedDateRangeChange={(s, e) => {
          setStart(s);
          setEnd(e);
        }}
        testId="past-picker"
      />
    </div>
  );
};

export const Disabled = () => (
  <div className="p-8">
    <DatePicker disabled testId="disabled-picker" />
  </div>
);

export const CustomPresets = () => {
  const [start, setStart] = useState<Date | undefined>();
  const [end, setEnd] = useState<Date | undefined>();
  const today = new Date();
  return (
    <div className="p-8">
      <DatePicker
        selectionMode="range"
        displayMenu
        selectedStartDate={start}
        selectedEndDate={end}
        onSelectedDateRangeChange={(s, e) => {
          setStart(s);
          setEnd(e);
        }}
        presets={[
          {
            label: "Last 7 Days",
            startDate: new Date(today.getFullYear(), today.getMonth(), today.getDate() - 7),
            endDate: today,
          },
          {
            label: "Last 30 Days",
            startDate: new Date(today.getFullYear(), today.getMonth(), today.getDate() - 30),
            endDate: today,
          },
        ]}
        testId="custom-preset-picker"
      />
    </div>
  );
};

export const MondayStart = () => {
  const [date, setDate] = useState<Date | undefined>();
  return (
    <div className="p-8">
      <DatePicker
        selectionMode="single"
        weekStartsOn={1}
        selectedDate={date}
        onSelectedDateChange={setDate}
        testId="monday-picker"
      />
    </div>
  );
};

export default {
  title: "Shared/DatePicker",
  component: DatePicker,
  parameters: {
    docs: {
      description: {
        component:
          "DatePicker component supporting single date and date range selection. " +
          "Features an optional preset menu, manual date input fields, timeframe filtering, " +
          "and configurable week start day.",
      },
    },
  },
  tags: ["autodocs"],
};
