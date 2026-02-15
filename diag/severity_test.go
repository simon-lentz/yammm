package diag

import "testing"

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		severity Severity
		want     string
	}{
		{Fatal, "fatal"},
		{Error, "error"},
		{Warning, "warning"},
		{Info, "info"},
		{Hint, "hint"},
		{Severity(255), "unknown"}, // Invalid severity
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.severity.String(); got != tt.want {
				t.Errorf("Severity(%d).String() = %q; want %q", tt.severity, got, tt.want)
			}
		})
	}
}

func TestSeverity_IsFailure(t *testing.T) {
	tests := []struct {
		severity Severity
		want     bool
	}{
		{Fatal, true},
		{Error, true},
		{Warning, false},
		{Info, false},
		{Hint, false},
	}

	for _, tt := range tests {
		t.Run(tt.severity.String(), func(t *testing.T) {
			if got := tt.severity.IsFailure(); got != tt.want {
				t.Errorf("%s.IsFailure() = %v; want %v", tt.severity, got, tt.want)
			}
		})
	}
}

func TestSeverity_IsMoreSevereThan(t *testing.T) {
	tests := []struct {
		name  string
		s     Severity
		other Severity
		want  bool
	}{
		{"Fatal more severe than Error", Fatal, Error, true},
		{"Fatal more severe than Warning", Fatal, Warning, true},
		{"Fatal more severe than Info", Fatal, Info, true},
		{"Fatal more severe than Hint", Fatal, Hint, true},
		{"Error more severe than Warning", Error, Warning, true},
		{"Error more severe than Info", Error, Info, true},
		{"Error more severe than Hint", Error, Hint, true},
		{"Warning more severe than Info", Warning, Info, true},
		{"Warning more severe than Hint", Warning, Hint, true},
		{"Info more severe than Hint", Info, Hint, true},

		{"Fatal not more severe than Fatal", Fatal, Fatal, false},
		{"Error not more severe than Fatal", Error, Fatal, false},
		{"Error not more severe than Error", Error, Error, false},
		{"Warning not more severe than Error", Warning, Error, false},
		{"Warning not more severe than Warning", Warning, Warning, false},
		{"Info not more severe than Warning", Info, Warning, false},
		{"Info not more severe than Info", Info, Info, false},
		{"Hint not more severe than Info", Hint, Info, false},
		{"Hint not more severe than Hint", Hint, Hint, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.IsMoreSevereThan(tt.other); got != tt.want {
				t.Errorf("%s.IsMoreSevereThan(%s) = %v; want %v", tt.s, tt.other, got, tt.want)
			}
		})
	}
}

func TestSeverity_IsAtLeastAsSevereAs(t *testing.T) {
	tests := []struct {
		name  string
		s     Severity
		other Severity
		want  bool
	}{
		// Same severity - should be true
		{"Fatal at least as severe as Fatal", Fatal, Fatal, true},
		{"Error at least as severe as Error", Error, Error, true},
		{"Warning at least as severe as Warning", Warning, Warning, true},
		{"Info at least as severe as Info", Info, Info, true},
		{"Hint at least as severe as Hint", Hint, Hint, true},

		// More severe - should be true
		{"Fatal at least as severe as Error", Fatal, Error, true},
		{"Fatal at least as severe as Hint", Fatal, Hint, true},
		{"Error at least as severe as Warning", Error, Warning, true},
		{"Warning at least as severe as Info", Warning, Info, true},
		{"Info at least as severe as Hint", Info, Hint, true},

		// Less severe - should be false
		{"Error not at least as severe as Fatal", Error, Fatal, false},
		{"Warning not at least as severe as Error", Warning, Error, false},
		{"Info not at least as severe as Warning", Info, Warning, false},
		{"Hint not at least as severe as Info", Hint, Info, false},
		{"Hint not at least as severe as Fatal", Hint, Fatal, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.IsAtLeastAsSevereAs(tt.other); got != tt.want {
				t.Errorf("%s.IsAtLeastAsSevereAs(%s) = %v; want %v", tt.s, tt.other, got, tt.want)
			}
		})
	}
}

func TestSeverity_Ordering(t *testing.T) {
	// Verify the ordering: Fatal < Error < Warning < Info < Hint
	if Fatal >= Error {
		t.Error("Fatal should be less than Error (more severe)")
	}
	if Error >= Warning {
		t.Error("Error should be less than Warning (more severe)")
	}
	if Warning >= Info {
		t.Error("Warning should be less than Info (more severe)")
	}
	if Info >= Hint {
		t.Error("Info should be less than Hint (more severe)")
	}
}

func TestSeverity_AllSeverities(t *testing.T) {
	// Verify all defined severities have unique string representations
	severities := []Severity{Fatal, Error, Warning, Info, Hint}
	seen := make(map[string]Severity)

	for _, s := range severities {
		str := s.String()
		if str == "unknown" {
			t.Errorf("Severity %d has unknown string", s)
		}
		if prev, ok := seen[str]; ok {
			t.Errorf("Duplicate string %q for severities %d and %d", str, prev, s)
		}
		seen[str] = s
	}
}
