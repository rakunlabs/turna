package statuscode

import "testing"

func TestParse(t *testing.T) {
	s, err := Parse("t", []string{"404", "500-503, 418"})
	if err != nil {
		t.Fatal(err)
	}

	for code, want := range map[int]bool{404: true, 418: true, 500: true, 503: true, 504: false, 200: false} {
		if s.Contains(code) != want {
			t.Fatalf("%d = %v", code, !want)
		}
	}

	if d, _ := Parse("t", nil, "502-504"); !d.Contains(503) {
		t.Fatal("default not used")
	}

	if s, err := Parse("t", []string{"500 - 599"}); err != nil || !s.Contains(550) {
		t.Fatalf("spaced range: %v", err)
	}

	for _, bad := range []string{"abc", "99", "600", "503-500", "5xx", "", " , ", "-5", "500-"} {
		if _, err := Parse("t", []string{bad}); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}
