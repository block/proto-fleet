package frigateupdate

import (
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// PackageKind is what an uploaded package installs. It is stored in
// frigate_update_package.kind, and a job takes the kind of its package.
type PackageKind string

const (
	// PackageKindFrigate is the controller software. The controllers stage it,
	// and a Start runs one coordinated rollout across the HA pair.
	PackageKindFrigate PackageKind = "frigate"
	// PackageKindProtoOS is the ProtoOS browser UI. Each controller installs
	// its copy alone as soon as the copy is committed.
	PackageKindProtoOS PackageKind = "proto_os"
)

// maxProtoOSPackageBytes is the largest proto-os package that Frigate accepts.
const maxProtoOSPackageBytes = int64(64) << 20

// kindRules are the rules that differ between the package kinds. Code that
// depends on the kind reads them from packageKinds.
type kindRules struct {
	// debianPackage is the package name in the release statement and in the
	// .deb control file.
	debianPackage string
	manifest      sdk.FrigatePackageKind
	// noun names the package in activity log entries, after a verb.
	noun string
	// runningEntry is the format of the activity log entry for a job that
	// becomes running. Its one %s is the target version.
	runningEntry string
	// maxSize caps Fleet's configured package size limit.
	maxSize int64
	// exactControlVersion requires the control file version to equal the
	// statement version. Without it, a Debian revision may follow.
	exactControlVersion bool
	// checkUpload checks the statement fields and the file name that only
	// this kind constrains.
	checkUpload func(st frigaterelease.Statement, filename string) error
	// releaseTag returns the manifest release tag and asset name of a package
	// version. Both are empty for a kind whose manifest does not carry them.
	releaseTag func(version string) (tag, asset string)
	// commitInstalls is true when a controller installs its copy as soon as the
	// copy commits. Fleet then sends no Start, and the first commit ends the
	// time in which a cancel is possible.
	commitInstalls bool
}

var packageKinds = map[PackageKind]kindRules{
	PackageKindFrigate: {
		debianPackage: frigaterelease.PackageName,
		manifest:      sdk.FrigatePackageFrigate,
		noun:          "controller software",
		runningEntry:  "The controller accepted the update to %s",
		maxSize:       frigaterelease.MaxPackageBytes,
		checkUpload:   func(frigaterelease.Statement, string) error { return nil },
		releaseTag:    func(string) (string, string) { return "", "" },
	},
	PackageKindProtoOS: {
		debianPackage:       frigaterelease.ProtoOSPackageName,
		manifest:            sdk.FrigatePackageProtoOS,
		noun:                "ProtoOS",
		runningEntry:        "The controllers committed the package for ProtoOS %s and are installing it",
		maxSize:             maxProtoOSPackageBytes,
		exactControlVersion: true,
		checkUpload:         checkProtoOSUpload,
		releaseTag: func(version string) (string, string) {
			tag, _ := sdk.ProtoOSReleaseTag(version)
			return tag, sdk.ProtoOSAssetName(tag)
		},
		commitInstalls: true,
	},
}

func (k PackageKind) rules() kindRules { return packageKinds[k] }

// kindOfStatement returns the kind of the package that a parsed statement
// names. frigaterelease accepts no other package name.
func kindOfStatement(st frigaterelease.Statement) PackageKind {
	for kind, rules := range packageKinds {
		if rules.debianPackage == st.Package {
			return kind
		}
	}
	return PackageKindFrigate
}

// checkProtoOSUpload applies the rules that Frigate applies to a proto-os
// manifest, so that Fleet refuses a package that no controller would install.
func checkProtoOSUpload(st frigaterelease.Statement, filename string) error {
	if st.Architecture != "all" {
		return fleeterror.NewInvalidArgumentErrorf("a ProtoOS release must be for architecture \"all\", not %q", st.Architecture)
	}
	if st.MinInstalledVersion != "" {
		return fleeterror.NewInvalidArgumentError("a ProtoOS release must not set min_installed_version")
	}
	tag, ok := sdk.ProtoOSReleaseTag(st.Version)
	if !ok {
		return fleeterror.NewInvalidArgumentErrorf("ProtoOS version %q does not come from a release tag", st.Version)
	}
	if want := sdk.ProtoOSAssetName(tag); filename != want {
		return fleeterror.NewInvalidArgumentErrorf("a ProtoOS package for %s must be named %q", tag, want)
	}
	return nil
}
