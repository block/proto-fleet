const FIRMWARE_FILENAME_VERSION_RE = /(?:^|[^0-9.])v?([0-9]+\.[0-9]+\.[0-9]+)(?:$|[^0-9.]|\.[A-Za-z])/;

export function firmwareVersionFromFilename(filename: string): string | null {
  return FIRMWARE_FILENAME_VERSION_RE.exec(filename)?.[1] ?? null;
}
