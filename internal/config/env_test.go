package config

import "testing"

func TestString(t *testing.T) {
	t.Setenv("CONFIG_STRING", " configured ")

	if got := String("CONFIG_STRING", "fallback"); got != "configured" {
		t.Fatalf("String() = %q; want configured", got)
	}
}

func TestIntRejectsInvalidValue(t *testing.T) {
	t.Setenv("CONFIG_INT", "invalid")

	if _, err := Int("CONFIG_INT", 10); err == nil {
		t.Fatal("Int() error = nil; want parse error")
	}
}
