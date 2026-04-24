const COMPLETED_SUFFIX = ".completed";

export const isCompletedEvent = (eventType: string): boolean => eventType.endsWith(COMPLETED_SUFFIX);

export const baseEventType = (eventType: string): string => eventType.replace(COMPLETED_SUFFIX, "");
