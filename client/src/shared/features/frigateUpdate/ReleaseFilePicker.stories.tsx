import { useState } from "react";
import ReleaseFilePicker from "./ReleaseFilePicker";
import {
  RELEASE_PACKAGE_FILE_KINDS,
  type ReleaseFileKind,
  type ReleaseFiles,
  type ReleasePackageFileKey,
} from "./releaseFiles";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";

export default {
  title: "Shared/Controller Software/Release File Picker",
  component: ReleaseFilePicker,
};

type FleetKey = ReleasePackageFileKey | "publicKey";
const fleetKinds: ReleaseFileKind<FleetKey>[] = [
  ...RELEASE_PACKAGE_FILE_KINDS,
  { key: "publicKey", extension: ".pem", label: "Public key", name: "public key", required: false },
];

const release = {
  deb: new File([new Uint8Array(72_565_730)], "frigate_0.2.20_linux_arm64.deb"),
  statement: new File([new Uint8Array(291)], "frigate_0.2.20_linux_arm64.deb.statement.json"),
  signature: new File([new Uint8Array(71)], "frigate_0.2.20_linux_arm64.deb.statement.sig"),
};

function Picker<Key extends string>({
  kinds,
  initial = {},
  emptyHint,
  disabled,
}: {
  kinds: ReleaseFileKind<Key>[];
  initial?: ReleaseFiles<Key>;
  emptyHint: string;
  disabled?: boolean;
}) {
  const [files, setFiles] = useState(initial);
  const [rejected, setRejected] = useState<string>();
  return (
    <div className="flex max-w-xl flex-col gap-4 p-6">
      {rejected ? <Callout intent="danger" prefixIcon={<Alert />} title={rejected} /> : null}
      <ReleaseFilePicker
        kinds={kinds}
        files={files}
        onChange={(next) => {
          setRejected(undefined);
          setFiles(next);
        }}
        onReject={setRejected}
        emptyHint={emptyHint}
        inputLabel="Controller software files"
        disabled={disabled}
      />
    </div>
  );
}

/** The three files of a release, as ProtoOS takes them. */
export const Empty = () => (
  <Picker kinds={RELEASE_PACKAGE_FILE_KINDS} emptyHint="Select the .deb, release statement and signature." />
);

export const SomeFilesChosen = () => (
  <Picker
    kinds={RELEASE_PACKAGE_FILE_KINDS}
    initial={{ deb: release.deb }}
    emptyHint="Select the .deb, release statement and signature."
  />
);

export const AllFilesChosen = () => (
  <Picker
    kinds={RELEASE_PACKAGE_FILE_KINDS}
    initial={release}
    emptyHint="Select the .deb, release statement and signature."
  />
);

/** Fleet also takes an optional public key. */
export const WithOptionalPublicKey = () => (
  <Picker
    kinds={fleetKinds}
    initial={{ ...release, publicKey: new File([new Uint8Array(178)], "frigate-release-2026.pem") }}
    emptyHint="Select the .deb, release statement and signature. Public key is optional."
  />
);

/** The files stay shown but can't be changed, for example while the release is read. */
export const Disabled = () => (
  <Picker
    kinds={RELEASE_PACKAGE_FILE_KINDS}
    initial={release}
    emptyHint="Select the .deb, release statement and signature."
    disabled
  />
);
