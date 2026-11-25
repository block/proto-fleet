package rollout

import (
	"fmt"

	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

// deviceNoun is what operator-facing copy calls a set of update targets.
type deviceNoun struct{ singular, plural string }

var (
	minerNoun     = deviceNoun{"miner", "miners"}
	containerNoun = deviceNoun{"container", "containers"}
	// anyDeviceNoun names targets where the text cannot tell miners from
	// containers, such as another channel's overlapping members.
	anyDeviceNoun = deviceNoun{"device", "devices"}
)

// nounFor names the targets of one manufacturer/model pair. A rollout covers
// one pair, and the Proto CT1 pair is a container.
func nounFor(manufacturer, model string) deviceNoun {
	if (files.FirmwareMetadata{TargetManufacturer: manufacturer, TargetModel: model}).IsCT1() {
		return containerNoun
	}
	return minerNoun
}

// of is the noun for n targets: singular for exactly one.
func (d deviceNoun) of(n int) string {
	if n == 1 {
		return d.singular
	}
	return d.plural
}

// count is "1 miner" or "3 containers".
func (d deviceNoun) count(n int) string { return fmt.Sprintf("%d %s", n, d.of(n)) }
