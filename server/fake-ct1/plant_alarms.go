package main

import (
	"net/http"
	"slices"

	"google.golang.org/grpc/codes"
)

// Plant alarms model the controller's PLC: an alarm latches when raised and the
// buzzer sounds. Mute only silences the buzzer. Reset clears latched alarms whose
// condition has cleared, and keeps those whose cause is still present. A newly
// raised alarm sounds the buzzer again.

const (
	severityWarning  = "SEVERITY_WARNING"
	severityCritical = "SEVERITY_CRITICAL"
)

type simulatedPlantAlarm struct {
	ID          string
	Name        string
	Scope       string
	Severity    string
	ComponentID string
	// ConditionPresent is true while the cause persists. A latched alarm whose
	// condition has cleared stays active until reset.
	ConditionPresent bool
}

type plantAlarm struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Scope       string `json:"scope,omitempty"`
	Severity    string `json:"severity"`
	ComponentID string `json:"componentId,omitempty"`
}

// demoPlantAlarms seeds one latched warning whose cause has cleared and one
// critical alarm whose cause is still present.
func demoPlantAlarms() []simulatedPlantAlarm {
	return []simulatedPlantAlarm{
		{ID: "coolant-low-pressure", Name: "Coolant low pressure", Scope: "plant", Severity: severityWarning, ComponentID: "coolant-pump"},
		{ID: "tank-b-outlet-over-temperature", Name: "Tank B outlet over temperature", Scope: "plant", Severity: severityCritical, ComponentID: "tank-b", ConditionPresent: true},
	}
}

func (s *ContainerState) plantAlarmsDTO() []plantAlarm {
	out := make([]plantAlarm, 0, len(s.plantAlarms))
	for _, alarm := range s.plantAlarms {
		out = append(out, plantAlarm{ID: alarm.ID, Name: alarm.Name, Scope: alarm.Scope, Severity: alarm.Severity, ComponentID: alarm.ComponentID})
	}
	return out
}

func (s *ContainerState) criticalAlarmActive() bool {
	return slices.ContainsFunc(s.plantAlarms, func(alarm simulatedPlantAlarm) bool { return alarm.Severity == severityCritical })
}

// raisePlantAlarm latches an alarm and re-sounds the buzzer, even after a mute.
func (s *ContainerState) raisePlantAlarm(alarm simulatedPlantAlarm) {
	alarm.ConditionPresent = true
	for i := range s.plantAlarms {
		if s.plantAlarms[i].ID == alarm.ID {
			s.plantAlarms[i].ConditionPresent = true
			return
		}
	}
	s.plantAlarms = append(s.plantAlarms, alarm)
	s.buzzerActive = true
}

// clearPlantAlarmCondition marks the cause gone; the alarm stays latched until reset.
func (s *ContainerState) clearPlantAlarmCondition(id string) {
	for i := range s.plantAlarms {
		if s.plantAlarms[i].ID == id {
			s.plantAlarms[i].ConditionPresent = false
		}
	}
}

// muteContainerAlarm silences the buzzer. Alarms stay active.
func (s *ContainerState) muteContainerAlarm() error {
	s.buzzerActive = false
	return nil
}

// resetContainerAlarm clears latched alarms whose condition has cleared. Hardware
// that clears alarm bits only with their conditions (the real CT1) does not offer it.
func (s *ContainerState) resetContainerAlarm() error {
	if s.Config.AlarmResetUnsupported {
		return &apiError{http.StatusNotImplemented, codes.Unimplemented, "alarm reset is not supported by this controller: alarms clear when their cause clears"}
	}
	s.plantAlarms = slices.DeleteFunc(s.plantAlarms, func(alarm simulatedPlantAlarm) bool { return !alarm.ConditionPresent })
	if len(s.plantAlarms) == 0 {
		s.buzzerActive = false
	}
	return nil
}
