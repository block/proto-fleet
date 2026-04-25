const COMPLETED_SUFFIX = ".completed";

export const isCompletedEvent = (eventType: string): boolean => eventType.endsWith(COMPLETED_SUFFIX);

export const baseEventType = (eventType: string): string =>
  isCompletedEvent(eventType) ? eventType.slice(0, -COMPLETED_SUFFIX.length) : eventType;
