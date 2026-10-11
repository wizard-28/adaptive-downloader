package concurrency

import (
	"errors"
	"sync"
	"testing"
)

func mustNew(t *testing.T, min, max int) *Controller {
	t.Helper()
	c, err := New(min, max)
	if err != nil {
		t.Fatalf("New(%d, %d) = %v", min, max, err)
	}
	return c
}

func TestInitialConcurrency(t *testing.T) {
	c := mustNew(t, 2, 8)
	if got := c.Current(); got != 2 {
		t.Fatalf("Current() = %d, want 2 (min)", got)
	}
	if got := c.Min(); got != 2 {
		t.Fatalf("Min() = %d, want 2", got)
	}
	if got := c.Max(); got != 8 {
		t.Fatalf("Max() = %d, want 8", got)
	}
}

func TestIncrease(t *testing.T) {
	c := mustNew(t, 1, 4)
	for want := 2; want <= 4; want++ {
		if got := c.Increase(); got != want {
			t.Fatalf("Increase() = %d, want %d", got, want)
		}
	}
}

func TestDecrease(t *testing.T) {
	c := mustNew(t, 1, 4)
	c.Set(3)
	if got := c.Decrease(); got != 2 {
		t.Fatalf("Decrease() = %d, want 2", got)
	}
	if got := c.Decrease(); got != 1 {
		t.Fatalf("Decrease() = %d, want 1", got)
	}
}

func TestRepeatedIncreaseAtMaximum(t *testing.T) {
	c := mustNew(t, 1, 3)
	for i := 0; i < 10; i++ {
		c.Increase()
	}
	if got := c.Current(); got != 3 {
		t.Fatalf("Current() = %d, want 3 (max)", got)
	}
}

func TestRepeatedDecreaseAtMinimum(t *testing.T) {
	c := mustNew(t, 2, 5)
	c.Set(3)
	for i := 0; i < 10; i++ {
		c.Decrease()
	}
	if got := c.Current(); got != 2 {
		t.Fatalf("Current() = %d, want 2 (min)", got)
	}
}

func TestMinEqualsMax(t *testing.T) {
	c := mustNew(t, 4, 4)
	if got := c.Current(); got != 4 {
		t.Fatalf("Current() = %d, want 4", got)
	}
	if got := c.Increase(); got != 4 {
		t.Fatalf("Increase() = %d, want 4", got)
	}
	if got := c.Decrease(); got != 4 {
		t.Fatalf("Decrease() = %d, want 4", got)
	}
	if got := c.Set(100); got != 4 {
		t.Fatalf("Set(100) = %d, want 4", got)
	}
}

func TestSetClamps(t *testing.T) {
	c := mustNew(t, 2, 6)
	if got := c.Set(0); got != 2 {
		t.Fatalf("Set(0) = %d, want 2 (min)", got)
	}
	if got := c.Set(100); got != 6 {
		t.Fatalf("Set(100) = %d, want 6 (max)", got)
	}
	if got := c.Set(4); got != 4 {
		t.Fatalf("Set(4) = %d, want 4", got)
	}
}

func TestInvalidConfig(t *testing.T) {
	cases := []struct {
		name     string
		min, max int
	}{
		{"zero min", 0, 4},
		{"negative min", -1, 4},
		{"max below min", 4, 3},
		{"zero max", 1, 0},
		{"both zero", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.min, tc.max); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New(%d, %d) = %v, want ErrInvalidConfig", tc.min, tc.max, err)
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	const (
		min, max   = 1, 16
		goroutines = 32
		ops        = 500
	)
	c := mustNew(t, min, max)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				switch i % 4 {
				case 0:
					c.Increase()
				case 1:
					c.Decrease()
				case 2:
					c.Set(i % (max + 5))
				default:
					_ = c.Current()
				}
			}
		}(g)
	}
	wg.Wait()

	if got := c.Current(); got < min || got > max {
		t.Fatalf("Current() = %d, want within [%d, %d]", got, min, max)
	}
}
