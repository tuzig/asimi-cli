package types

import "testing"

func TestRealmConstants(t *testing.T) {
	if Earth != Realm("earth") {
		t.Errorf("Earth = %q, want %q", Earth, "earth")
	}
	if Heaven != Realm("heaven") {
		t.Errorf("Heaven = %q, want %q", Heaven, "heaven")
	}
	if Intent != Realm("intent") {
		t.Errorf("Intent = %q, want %q", Intent, "intent")
	}
}

func TestRealmStringConversion(t *testing.T) {
	// Realm is a string type, so it should be convertible to/from string
	s := string(Earth)
	if s != "earth" {
		t.Errorf("string(Earth) = %q, want %q", s, "earth")
	}
}

func TestRealmValuesDistinct(t *testing.T) {
	if Earth == Heaven {
		t.Error("Earth and Heaven should be distinct")
	}
	if Earth == Intent {
		t.Error("Earth and Intent should be distinct")
	}
	if Heaven == Intent {
		t.Error("Heaven and Intent should be distinct")
	}
}
