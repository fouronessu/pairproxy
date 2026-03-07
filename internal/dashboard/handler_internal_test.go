// Package dashboard — internal tests (same package, can access unexported symbols).
package dashboard

import (
	"testing"
)

// ---------------------------------------------------------------------------
// parseOptionalInt — (33.3% coverage → 提升)
// ---------------------------------------------------------------------------

func TestParseOptionalInt_EmptyString(t *testing.T) {
	result := parseOptionalInt("")
	if result != nil {
		t.Errorf("parseOptionalInt(\"\") = %v, want nil", result)
	}
}

func TestParseOptionalInt_ValidPositiveInt(t *testing.T) {
	result := parseOptionalInt("42")
	if result == nil {
		t.Fatal("parseOptionalInt(\"42\") = nil, want *42")
	}
	if *result != 42 {
		t.Errorf("parseOptionalInt(\"42\") = %d, want 42", *result)
	}
}

func TestParseOptionalInt_ValidOne(t *testing.T) {
	result := parseOptionalInt("1")
	if result == nil {
		t.Fatal("parseOptionalInt(\"1\") = nil, want *1")
	}
	if *result != 1 {
		t.Errorf("parseOptionalInt(\"1\") = %d, want 1", *result)
	}
}

func TestParseOptionalInt_InvalidString(t *testing.T) {
	for _, s := range []string{"abc", "1.5", "not-a-number", " 42", "42abc"} {
		result := parseOptionalInt(s)
		if result != nil {
			t.Errorf("parseOptionalInt(%q) = %v, want nil", s, result)
		}
	}
}

func TestParseOptionalInt_ZeroOrNegative(t *testing.T) {
	// 0 和负数应返回 nil（根据 v <= 0 判断）
	for _, s := range []string{"0", "-1", "-100"} {
		result := parseOptionalInt(s)
		if result != nil {
			t.Errorf("parseOptionalInt(%q) = %v, want nil (v <= 0)", s, result)
		}
	}
}

func TestParseOptionalInt_LargeNumber(t *testing.T) {
	result := parseOptionalInt("1000000")
	if result == nil {
		t.Fatal("parseOptionalInt(\"1000000\") = nil, want *1000000")
	}
	if *result != 1000000 {
		t.Errorf("parseOptionalInt(\"1000000\") = %d, want 1000000", *result)
	}
}
