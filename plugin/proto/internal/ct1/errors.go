package ct1

import (
	"cmp"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
)

// An error here is one firmware cause or plant alarm on one component, at any
// severity. Fleet closes an error that polls stop reporting, so a report must
// separate "this source says the error cleared" from "this source said
// nothing". Each error remembers the evidence sources that asserted it and
// stays reported until every one of them supplies a fresh reading without it,
// or leaves the container. Frigate omits board health older than two minutes
// from the map, so a silent board is the normal shape of missing evidence.

// errorEvidenceMaxAge lets error polling reuse the observation that the same
// poll cycle's telemetry read just made, instead of reading the map twice.
const errorEvidenceMaxAge = 10 * time.Second

const (
	// Frigate lists every active alarm. A longer or malformed list is not a
	// complete reading, so omitted alarms are never treated as cleared.
	maxPlantAlarms         = 128
	maxPlantAlarmTextBytes = 256
	// Errors carried for silent sources are bounded per container.
	maxRetainedErrorItems = 8192
	// Fleet Node rejects a whole result that exceeds its field limits.
	maxErrorSummaryBytes = 1024
	maxVendorCodeBytes   = 255
)

const (
	// boardIssueComponent is the module issue component of a single board.
	boardIssueComponent = "hashboard"

	plantAlarmsSource    = "plant-alarms"
	plantAggregateSource = "plant-aggregate"

	errorEvidenceCurrent  = "current"
	errorEvidenceRetained = "retained"
)

type errorComponent struct {
	kind sdkerrors.ComponentType
	id   string
}

// errorItemKey keeps the firmware cause. Several causes can share one Fleet
// error code on a component; they are merged only when reported.
type errorItemKey struct {
	component errorComponent
	cause     string
}

// errorPlace is where operators find a component. Positions are selectors and
// can change without changing the component's identity.
type errorPlace struct {
	label string
	attrs map[string]string
}

type errorItem struct {
	key      errorItemKey
	code     sdkerrors.MinerError
	severity sdkerrors.Severity
	text     string
	place    errorPlace
	// sources asserted this error and have not since supplied a fresh reading
	// without it.
	sources     []string
	firstSeen   time.Time
	confirmedAt time.Time
	// confirmed means the newest evidence asserted the error; otherwise it is
	// carried for sources that are silent.
	confirmed bool
}

// errorEvidence is one collection's view of every error source.
type errorEvidence struct {
	observedAt time.Time
	items      map[errorItemKey]*errorItem
	// fresh sources supplied an authoritative reading: an error they do not
	// assert has cleared for them.
	fresh map[string]bool
	// present sources still belong to the container but may be silent. Frigate
	// lists every board it has a location or inventory record for, so a board
	// that is not listed has been removed.
	present map[string]bool
	// unresolved means no missing source is treated as removed: Frigate either
	// reported a board that was rejected without a usable identity, which
	// could be any of them, or listed no board at all.
	unresolved bool
	places     map[errorComponent]errorPlace
}

type errorCollector struct {
	evidence errorEvidence
	// listed means the map named at least one board.
	listed bool
	// Unlocated boards have no module issue report. Budget them separately so
	// error polling never changes what telemetry retains.
	budget issueBudget
}

func newErrorCollector(observedAt time.Time) *errorCollector {
	return &errorCollector{
		evidence: errorEvidence{
			observedAt: observedAt,
			items:      map[errorItemKey]*errorItem{},
			fresh:      map[string]bool{},
			present:    map[string]bool{},
			places:     map[errorComponent]errorPlace{},
		},
		budget: issueBudget{remaining: maxContainerIssueBytes},
	}
}

func (c *errorCollector) result() *errorEvidence {
	if c == nil {
		return nil
	}
	// A map that lists no board at all cannot show that every board left.
	if !c.listed {
		c.evidence.unresolved = true
	}
	return &c.evidence
}

func (c *errorCollector) board(mac string) {
	c.listed = true
	c.evidence.present["board:"+mac] = true
}

func boardComponent(mac string) errorComponent {
	return errorComponent{kind: sdkerrors.ComponentTypeHashBoard, id: "mac:" + mac}
}

// module records one located module. Boards are selected by MAC and the shared
// PSU by position, matching Fleet inventory.
func (c *errorCollector) module(tank, module int32, hashboards []sdk.ContainerHashboard, issues *sdk.ContainerModuleIssues, origins issueOrigins) {
	if c == nil {
		return
	}
	e := &c.evidence
	position := map[string]string{"tank": strconv.Itoa(int(tank)), "module": strconv.Itoa(int(module))}
	psu := errorComponent{kind: sdkerrors.ComponentTypePSU, id: fmt.Sprintf("tank:%d/module:%d", tank, module)}
	psuSource := fmt.Sprintf("psu:%d/%d", tank, module)
	e.present[psuSource] = true
	e.places[psu] = errorPlace{label: fmt.Sprintf("Tank %d, module %d PSU", tank, module), attrs: position}
	for _, board := range hashboards {
		switch {
		case board.Occupancy == "unknown":
			c.rejected(board)
		case board.MACAddress != "":
			// Only an accepted, occupied slot carries an identity.
			c.board(board.MACAddress)
			attrs := maps.Clone(position)
			attrs["hashboard_position"] = strconv.Itoa(int(board.Position))
			attrs["mac_address"] = board.MACAddress
			e.places[boardComponent(board.MACAddress)] = errorPlace{
				label: fmt.Sprintf("Tank %d, module %d, hashboard %d (%s)", tank, module, board.Position, board.MACAddress),
				attrs: attrs,
			}
		}
	}
	c.issues(issues, origins, psu, psuSource)
}

func unlocatedPSUSource(mac string) string { return "psu:board:" + mac }

// rejected records a slot whose observation was refused. A claimed identity
// is still in the container and merely silent. A slot Frigate did not report
// at all claims nothing: outside switch_ports mode that is how an emptied
// position appears.
func (c *errorCollector) rejected(observation sdk.ContainerHashboard) {
	e := &c.evidence
	claimed := false
	for _, claim := range observation.IdentityClaims {
		if claim.MACAddress != "" {
			c.board(claim.MACAddress)
			e.present[unlocatedPSUSource(claim.MACAddress)] = true
			claimed = true
		}
	}
	if !claimed && observation.UnavailableReason != "" {
		e.unresolved = true
	}
}

// unlocated records switch_ports inventory. Without a module, PSU evidence
// stays with the board that reported it.
func (c *errorCollector) unlocated(board *Board, observation sdk.ContainerHashboard, now time.Time) {
	if c == nil {
		return
	}
	e := &c.evidence
	if board == nil || observation.MACAddress == "" {
		if observation.Occupancy == "unknown" {
			c.rejected(observation)
		}
		return
	}
	mac := observation.MACAddress
	psu := errorComponent{kind: sdkerrors.ComponentTypePSU, id: "mac:" + mac}
	psuSource := unlocatedPSUSource(mac)
	attrs := map[string]string{"mac_address": mac}
	c.board(mac)
	e.present[psuSource] = true
	e.places[boardComponent(mac)] = errorPlace{label: "Unlocated hashboard " + mac, attrs: attrs}
	e.places[psu] = errorPlace{label: "PSU of unlocated hashboard " + mac, attrs: attrs}
	issues, origins := moduleIssueEvidence([]Board{*board}, now, false, &c.budget)
	c.issues(issues, origins, psu, psuSource)
}

// issues adds one module issue report. SourceTimes lists the samples that were
// fresh; the protobuf path always supplies both cause arrays, so a listed
// board sample is a complete reading of that board.
func (c *errorCollector) issues(issues *sdk.ContainerModuleIssues, origins issueOrigins, psu errorComponent, psuSource string) {
	if issues == nil {
		return
	}
	e := &c.evidence
	source := func(origin string) string {
		if origin == "psu" {
			return psuSource
		}
		if mac, ok := strings.CutPrefix(origin, "hashboard:"); ok && mac != "" {
			return "board:" + mac
		}
		return ""
	}
	for origin := range issues.SourceTimes {
		if name := source(origin); name != "" {
			e.fresh[name] = true
		}
	}
	for _, issue := range issues.Items {
		component := psu
		switch issue.Component {
		case "psu":
		case boardIssueComponent:
			if issue.ComponentID == "" {
				continue
			}
			component = boardComponent(issue.ComponentID)
		default:
			continue
		}
		if issue.Severity != "fault" && issue.Severity != "warning" {
			continue
		}
		code, severity := canonicalIssue(issue.Cause, issue.Severity == "warning")
		item := &errorItem{
			key:  errorItemKey{component: component, cause: issue.Cause},
			code: code, severity: severity, text: causeText(issue.Cause), place: e.places[component],
			firstSeen: issue.ObservedAt, confirmedAt: issue.ObservedAt, confirmed: true,
		}
		for _, origin := range origins[issueOriginKey(issue)] {
			if name := source(origin); name != "" && !slices.Contains(item.sources, name) {
				item.sources = append(item.sources, name)
			}
		}
		// An error that cannot name its source could never be withdrawn.
		if len(item.sources) > 0 {
			e.items[item.key] = item
		}
	}
}

// plant records active plant alarms. The response timestamp is the oldest
// contributing poll, so an old timestamp means Frigate is serving a retained
// snapshot. A failed, stale, or plant-less read is silence: it neither adds
// nor clears alarms, and board and PSU evidence is unaffected.
func (c *errorCollector) plant(wire *mdk.GetContainerHealthResponse, err error, now time.Time) {
	if c == nil {
		return
	}
	e := &c.evidence
	e.present[plantAlarmsSource], e.present[plantAggregateSource] = true, true
	at := sourceTime(wire.GetTimestamp())
	plant := wire.GetPlant()
	if err != nil || plant == nil || at.IsZero() || now.Sub(at) > healthMaxAge || at.Sub(now) > clockSkewAllowance {
		return
	}
	if at.After(now) {
		at = now
	}
	add := func(id, cause, text string, severity sdkerrors.Severity, place errorPlace, source string) {
		key := errorItemKey{component: errorComponent{kind: sdkerrors.ComponentTypeUnspecified, id: id}, cause: cause}
		e.items[key] = &errorItem{
			key: key, code: sdkerrors.VendorErrorUnmapped, severity: severity, text: text, place: place,
			sources: []string{source}, firstSeen: at, confirmedAt: at, confirmed: true,
		}
	}
	e.fresh[plantAggregateSource] = true
	alarms := wire.GetAlarms()
	if len(alarms) == 0 && (plant.GetAlarmActive() || plant.GetCriticalAlarmActive()) {
		// Frigate lists alarms only from a live plant poll. Before its first
		// poll it serves the saved flags alone; an empty list is then not a
		// clear reading of the alarms Fleet already holds.
		cause, text, severity := "PLANT_ALARM_ACTIVE", "Alarm active; the controller did not list its alarms", sdkerrors.SeverityMajor
		if plant.GetCriticalAlarmActive() {
			cause, text, severity = "PLANT_CRITICAL_ALARM_ACTIVE", "Critical alarm active; the controller did not list its alarms", sdkerrors.SeverityCritical
		}
		add("plant", cause, text, severity, errorPlace{label: "Plant"}, plantAggregateSource)
		return
	}
	complete := len(alarms) <= maxPlantAlarms
	for _, alarm := range alarms[:min(len(alarms), maxPlantAlarms)] {
		id, name := strings.TrimSpace(alarm.GetId()), strings.TrimSpace(alarm.GetName())
		scope, component := strings.TrimSpace(alarm.GetScope()), strings.TrimSpace(alarm.GetComponentId())
		if id == "" || len(id) > maxIssueComponentIDBytes || len(name) > maxPlantAlarmTextBytes || len(scope) > maxPlantAlarmTextBytes || len(component) > maxPlantAlarmTextBytes {
			complete = false
			continue
		}
		text := cmp.Or(name, id)
		attrs := map[string]string{"plant_alarm_severity": alarm.GetSeverity().String()}
		if scope != "" {
			attrs["plant_alarm_scope"] = scope
		}
		if component != "" {
			attrs["plant_component_id"] = component
			text += " (component " + component + ")"
		}
		add("plant-alarm:"+id, id, text, plantAlarmSeverity(alarm.GetSeverity()), errorPlace{label: "Plant alarm", attrs: attrs}, plantAlarmsSource)
	}
	if complete {
		e.fresh[plantAlarmsSource] = true
	}
}

// Frigate plant alarms have no Fleet code. Informational notices stay
// informational; an alarm without a reported severity is not hidden as one.
func plantAlarmSeverity(severity mdk.Severity) sdkerrors.Severity {
	switch severity {
	case mdk.Severity_SEVERITY_CRITICAL:
		return sdkerrors.SeverityCritical
	case mdk.Severity_SEVERITY_ERROR:
		return sdkerrors.SeverityMajor
	case mdk.Severity_SEVERITY_INFO:
		return sdkerrors.SeverityInfo
	case mdk.Severity_SEVERITY_WARNING, mdk.Severity_SEVERITY_UNSPECIFIED:
		return sdkerrors.SeverityMinor
	}
	return sdkerrors.SeverityMinor
}

var causeAcronyms = map[string]string{"asic": "ASIC", "psu": "PSU", "usb": "USB", "ecc": "ECC", "mc3": "MC3"}

// causeText makes a firmware cause readable. The exact spelling stays in the
// vendor code.
func causeText(cause string) string {
	words := strings.Fields(strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(cause, "HASHBOARD_ERROR_CAUSE_"), "_", " ")))
	if len(words) == 0 {
		return "Unspecified firmware cause"
	}
	for i, word := range words {
		if acronym, ok := causeAcronyms[word]; ok {
			words[i] = acronym
		} else if i == 0 {
			first, size := utf8.DecodeRuneInString(word)
			words[i] = string(unicode.ToUpper(first)) + word[size:]
		}
	}
	return strings.Join(words, " ")
}

// joinBounded keeps whole parts and counts the ones that do not fit.
func joinBounded(parts []string, separator string, limit int) string {
	const reserve = len(" (+0000 more)")
	out := ""
	for i, part := range parts {
		if i == 0 {
			out = part
			continue
		}
		if len(out)+len(separator)+len(part) > limit-reserve {
			return out + fmt.Sprintf(" (+%d more)", len(parts)-i)
		}
		out += separator + part
	}
	return out
}

// errorState is one container's reported errors, including those carried for
// silent sources. It is shared by every handle for the container in this
// process, including Fleet Node's per-command handles, and does not survive a
// plugin restart.
type errorState struct {
	mu         sync.Mutex
	observedAt time.Time
	items      map[errorItemKey]*errorItem
}

var errorStates = struct {
	sync.Mutex
	byContainer map[string]*errorState
}{byContainer: map[string]*errorState{}}

func errorStateFor(container string) *errorState {
	errorStates.Lock()
	defer errorStates.Unlock()
	state := errorStates.byContainer[container]
	if state == nil {
		state = &errorState{}
		errorStates.byContainer[container] = state
	}
	return state
}

// current reports whether evidence newer than maxAge has been observed.
func (s *errorState) current(now time.Time, maxAge time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.observedAt.IsZero() && now.Sub(s.observedAt) < maxAge
}

// observe reconciles the container's errors with newer evidence. Telemetry and
// error polls both feed it, so an error that a board withdrew before going
// silent is not carried.
func (s *errorState) observe(e *errorEvidence) {
	if e == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.observedAt.Before(s.observedAt) {
		return
	}
	// A silent source keeps its errors while it still belongs to the container.
	silent := func(source string) bool {
		return !e.fresh[source] && (e.present[source] || e.unresolved)
	}
	next := make(map[errorItemKey]*errorItem, len(e.items))
	for key, current := range e.items {
		item := *current
		item.sources = slices.Clone(current.sources)
		if previous := s.items[key]; previous != nil {
			if !previous.firstSeen.IsZero() && previous.firstSeen.Before(item.firstSeen) {
				item.firstSeen = previous.firstSeen
			}
			for _, source := range previous.sources {
				if silent(source) && !slices.Contains(item.sources, source) {
					item.sources = append(item.sources, source)
				}
			}
		}
		next[key] = &item
	}
	for key, previous := range s.items {
		if next[key] != nil {
			continue
		}
		var sources []string
		for _, source := range previous.sources {
			if silent(source) {
				sources = append(sources, source)
			}
		}
		if len(sources) == 0 {
			continue
		}
		item := *previous
		item.sources, item.confirmed = sources, false
		if place, ok := e.places[key.component]; ok {
			item.place = place
		}
		next[key] = &item
	}
	if excess := len(next) - maxRetainedErrorItems; excess > 0 {
		carried := make([]*errorItem, 0, len(next))
		for _, item := range next {
			if !item.confirmed {
				carried = append(carried, item)
			}
		}
		slices.SortFunc(carried, func(a, b *errorItem) int { return a.confirmedAt.Compare(b.confirmedAt) })
		dropped := min(excess, len(carried))
		for _, item := range carried[:dropped] {
			delete(next, item.key)
		}
		slog.Warn("CT1 dropped the oldest errors carried for silent sources", "dropped", dropped, "limit", maxRetainedErrorItems)
	}
	s.items, s.observedAt = next, e.observedAt
}

// report renders the container's errors in Fleet's error contract. Fleet
// identifies an error by code and component, so causes sharing both are
// reported as one.
func (s *errorState) report(deviceID string) sdk.DeviceErrors {
	s.mu.Lock()
	defer s.mu.Unlock()
	type group struct {
		code      sdkerrors.MinerError
		component errorComponent
	}
	groups := map[group][]*errorItem{}
	for _, item := range s.items {
		key := group{code: item.code, component: item.key.component}
		groups[key] = append(groups[key], item)
	}
	out := sdk.DeviceErrors{DeviceID: deviceID, Errors: make([]sdk.DeviceError, 0, len(groups))}
	for key, items := range groups {
		slices.SortFunc(items, func(a, b *errorItem) int {
			return cmp.Or(cmp.Compare(a.severity, b.severity), cmp.Compare(a.key.cause, b.key.cause))
		})
		// Severity values order most severe first, so the first item decides.
		first := items[0]
		reported := sdk.DeviceError{
			MinerError: key.code, Severity: first.severity, DeviceID: deviceID,
			ComponentID: new(key.component.id), ComponentType: key.component.kind,
			FirstSeenAt: first.firstSeen, VendorAttributes: maps.Clone(first.place.attrs),
		}
		if reported.VendorAttributes == nil {
			reported.VendorAttributes = map[string]string{}
		}
		causes, texts := make([]string, 0, len(items)), make([]string, 0, len(items))
		confirmed, confirmedAt := false, time.Time{}
		for _, item := range items {
			causes, texts = append(causes, item.key.cause), append(texts, item.text)
			confirmed = confirmed || item.confirmed
			if item.confirmedAt.After(confirmedAt) {
				confirmedAt = item.confirmedAt
			}
			if !item.firstSeen.IsZero() && item.firstSeen.Before(reported.FirstSeenAt) {
				reported.FirstSeenAt = item.firstSeen
			}
		}
		reported.CauseSummary = joinBounded(texts, "; ", maxErrorSummaryBytes)
		reported.Summary = first.place.label + ": " + reported.CauseSummary
		reported.VendorAttributes["vendor_code"] = joinBounded(causes, ",", maxVendorCodeBytes)
		if confirmed {
			// The store advances last-seen on every report; only a confirmed
			// error claims an observation time of its own.
			reported.LastSeenAt = s.observedAt
			reported.VendorAttributes["ct1_evidence"] = errorEvidenceCurrent
		} else {
			// The marker and its time are fixed while the source is silent, so
			// the stored error is rewritten once, not on every poll.
			at := confirmedAt.UTC().Format(time.RFC3339)
			reported.Summary += " (source not reporting; last confirmed " + at + ")"
			reported.VendorAttributes["ct1_evidence"] = errorEvidenceRetained
			reported.VendorAttributes["ct1_last_confirmed_at"] = at
		}
		out.Errors = append(out.Errors, reported)
	}
	// Fleet Node keeps the first reports when a result must be truncated.
	slices.SortFunc(out.Errors, func(a, b sdk.DeviceError) int {
		return cmp.Or(
			cmp.Compare(a.Severity, b.Severity),
			cmp.Compare(a.ComponentType, b.ComponentType),
			cmp.Compare(*a.ComponentID, *b.ComponentID),
			cmp.Compare(a.MinerError, b.MinerError),
		)
	})
	return out
}
