package services

import "testing"

func TestUpdateServiceVersion(t *testing.T) {
	service := NewUpdateService(nil, "1.2.3")
	if got := service.Version(); got != "1.2.3" {
		t.Fatalf("Version() = %q, want %q", got, "1.2.3")
	}
}
