package service

import (
	"strings"
	"sync"
	"testing"

	"github.com/rakunlabs/turna/pkg/runner"
)

func TestTTYRejectsFilters(t *testing.T) {
	for _, s := range []Service{
		{Name: "filter", TTY: true, Filters: [][]byte{[]byte("secret")}},
		{Name: "filter-values", TTY: true, FiltersValues: []string{"secrets"}},
	} {
		if err := s.Register(); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("expected filter configuration error, got %v", err)
		}
	}
}

func TestTTYRegistersWithoutOutputFilter(t *testing.T) {
	previous := runner.GlobalReg
	t.Cleanup(func() { runner.GlobalReg = previous })
	runner.NewStoreReg(&sync.WaitGroup{}).SetAsGlobal()
	s := Service{Name: "shell", Command: "bash -i", TTY: true}
	if err := s.Register(); err != nil {
		t.Fatal(err)
	}
	c := runner.GlobalReg.Get("shell")
	if !c.TTY || c.Filter != nil {
		t.Fatal("tty must bypass output filtering")
	}
}
