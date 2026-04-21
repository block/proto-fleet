export const SCHEDULES_CHANGED_EVENT = "protoFleet:schedules-changed";

export const emitSchedulesChanged = () => {
  if (typeof window === "undefined") {
    return;
  }

  window.dispatchEvent(new CustomEvent(SCHEDULES_CHANGED_EVENT));
};
