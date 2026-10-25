package main

import (
	"reflect"
	"testing"
)

// The custom spec used in Stage A must reproduce the built-in d2 schedule.
func TestScheduleSpecMatchesD2(t *testing.T) {
	got, err := parseScheduleSpec("2:163840,4:2560000")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d2Phases) {
		t.Fatalf("got %v, want %v", got, d2Phases)
	}
}

func TestScheduleSpecRejectsBadInput(t *testing.T) {
	for _, spec := range []string{"", "2", "0:100", "2:0", "3:1,2:5", "x:1", "2:y"} {
		if _, err := parseScheduleSpec(spec); err == nil {
			t.Errorf("%q: want error", spec)
		}
	}
}
