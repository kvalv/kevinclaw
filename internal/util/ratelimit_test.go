package util

import "testing"

func TestPerHour(t *testing.T) {
	r := NewPerHour(3)

	if !r.Allow("ch1") {
		t.Fatal("1st should be allowed")
	}
	if !r.Allow("ch1") {
		t.Fatal("2nd should be allowed")
	}
	if !r.Allow("ch1") {
		t.Fatal("3rd should be allowed")
	}
	if r.Allow("ch1") {
		t.Fatal("4th should be rejected")
	}

	// Different key should be independent
	if !r.Allow("ch2") {
		t.Fatal("ch2 should be allowed")
	}
}
