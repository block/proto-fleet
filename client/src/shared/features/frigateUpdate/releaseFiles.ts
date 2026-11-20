/**
 * One kind of file in a controller software release. A release is one file of each kind, and the kinds are told
 * apart by extension. `name` is the kind in a sentence.
 */
export interface ReleaseFileKind<Key extends string = string> {
  key: Key;
  extension: string;
  label: string;
  name: string;
  required: boolean;
}

/** The files chosen so far, by kind. */
export type ReleaseFiles<Key extends string = string> = Partial<Record<Key, File>>;

export type ReleasePackageFileKey = "deb" | "statement" | "signature";

/** The three files every controller software release has. Fleet adds an optional public key. */
export const RELEASE_PACKAGE_FILE_KINDS: ReleaseFileKind<ReleasePackageFileKey>[] = [
  { key: "deb", extension: ".deb", label: "Debian package (.deb)", name: "package", required: true },
  { key: "statement", extension: ".json", label: "Release statement", name: "release statement", required: true },
  { key: "signature", extension: ".sig", label: "Release signature", name: "release signature", required: true },
];

/** Sorts one selection by kind, or says why it can't be used. */
export function sortReleaseFiles<Key extends string>(
  kinds: ReleaseFileKind<Key>[],
  selected: File[],
): { files: ReleaseFiles<Key> } | { error: string } {
  const files: ReleaseFiles<Key> = {};
  for (const file of selected) {
    const kind = kinds.find(({ extension }) => file.name.toLowerCase().endsWith(extension));
    if (!kind) return { error: `${file.name} is not a supported file type.` };
    if (files[kind.key]) return { error: `Select only one ${kind.name}.` };
    files[kind.key] = file;
  }
  return { files };
}

/** The names of the required kinds that have no file yet. */
export function missingReleaseFiles<Key extends string>(kinds: ReleaseFileKind<Key>[], files: ReleaseFiles<Key>) {
  return kinds.filter((kind) => kind.required && !files[kind.key]).map((kind) => kind.name);
}
