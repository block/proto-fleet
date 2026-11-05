// Package poolconfigurations defines ordered pool settings independently of
// their account owner and hardware application mechanism.
package poolconfigurations

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"buf.build/go/protovalidate"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
	"github.com/google/uuid"
)

type SlotOptions struct {
	// ID identifies a slot within one configuration across priority changes.
	// Empty requests a new server-issued identity.
	ID                            string
	URL, Username, WorkerTemplate string
	// Nil preserves the matching slot's password if its destination and username
	// are unchanged. An explicit empty value clears the password.
	Password *string
}

type RevisionOptions struct {
	Name  string
	Slots []SlotOptions
}

func Validate(options RevisionOptions) (RevisionOptions, error) {
	options.Name = strings.TrimSpace(options.Name)
	if options.Name == "" || !utf8.ValidString(options.Name) || utf8.RuneCountInString(options.Name) > 255 {
		return options, fleeterror.NewInvalidArgumentError("pool name must contain 1 to 255 characters")
	}
	if len(options.Slots) < 1 || len(options.Slots) > 3 {
		return options, fleeterror.NewInvalidArgumentError("pool configurations require 1 to 3 ordered slots")
	}
	for _, slot := range options.Slots {
		if slot.ID != "" {
			id, err := uuid.Parse(slot.ID)
			if err != nil || id.String() != slot.ID {
				return options, fleeterror.NewInvalidArgumentError("invalid pool slot identity")
			}
		}
		if !ValidURL(slot.URL) {
			return options, fleeterror.NewInvalidArgumentError("invalid pool destination")
		}
		if !validPoolText(slot.Username, 255, false) || slot.Username == "" {
			return options, fleeterror.NewInvalidArgumentError("invalid payout username")
		}
		if slot.Password != nil && !validPoolText(*slot.Password, 255, true) {
			return options, fleeterror.NewInvalidArgumentError("invalid pool password")
		}
		if err := validateWorkerTemplate(slot.WorkerTemplate); err != nil {
			return options, err
		}
	}
	return options, nil
}

func validPoolText(value string, maxBytes int, allowSpace bool) bool {
	if len(value) > maxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || (!allowSpace && unicode.IsSpace(r)) {
			return false
		}
	}
	return true
}

func ValidURL(raw string) bool {
	// Use the canonical pool input contract as the common destination grammar. Additional
	// checks below reject malformed hosts, unsafe ports and invalid V2 keys.
	if !validPoolText(raw, 256, false) || protovalidate.Validate(&poolspb.PoolInput{Url: raw, Username: "validation"}) != nil {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || u.Opaque != "" || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "stratum+tcp", "stratum+ssl", "stratum+ws", "stratum2+tcp":
	default:
		return false
	}
	host := u.Hostname()
	if net.ParseIP(host) == nil {
		if len(host) == 0 || len(host) > 253 {
			return false
		}
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return false
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return false
				}
			}
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	if u.Scheme == "stratum2+tcp" {
		return u.Port() != "" && sv2.ValidatePoolURL(raw) == nil
	}
	return u.Path == "" || u.Path == "/"
}

func validateWorkerTemplate(template string) error {
	if !validPoolText(template, 128, false) {
		return fleeterror.NewInvalidArgumentError("invalid worker template")
	}
	for remaining := template; remaining != ""; {
		index := strings.IndexAny(remaining, "{}")
		if index < 0 {
			break
		}
		remaining = remaining[index:]
		switch {
		case strings.HasPrefix(remaining, "{miner_id}"):
			remaining = strings.TrimPrefix(remaining, "{miner_id}")
		case strings.HasPrefix(remaining, "{miner_name}"):
			remaining = strings.TrimPrefix(remaining, "{miner_name}")
		default:
			return fleeterror.NewInvalidArgumentError("invalid worker template placeholder")
		}
	}
	return nil
}

// RenderWorker keeps the complete payout username, including its periods.
// Future allocation code supplies stable miner identifiers and names.
func RenderWorker(username, template, minerID, minerName string) (string, error) {
	if username == "" || !validPoolText(username, 255, false) {
		return "", fleeterror.NewInvalidArgumentError("invalid payout username")
	}
	if err := validateWorkerTemplate(template); err != nil {
		return "", err
	}
	worker := username
	if template != "" {
		suffix := strings.NewReplacer("{miner_id}", minerID, "{miner_name}", minerName).Replace(template)
		worker += "." + suffix
	}
	if !validPoolText(worker, 255, false) {
		return "", fleeterror.NewInvalidArgumentError("rendered pool worker must contain at most 255 bytes without whitespace or controls")
	}
	return worker, nil
}
