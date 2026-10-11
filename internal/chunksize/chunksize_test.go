package chunksize

import (
	"errors"
	"sync"
	"testing"
)

const (
	kiB = int64(1024)
)

func mustNew(t *testing.T, min, max, initial, step int64) *Controller {
	t.Helper()
	c, err := New(min, max, initial, step)
	if err != nil {
		t.Fatalf("New(%d, %d, %d, %d) = %v", min, max, initial, step, err)
	}
	return c
}

func TestInitialTarget(t *testing.T) {
	c := mustNew(t, 16*kiB, 128*kiB, 32*kiB, 16*kiB)
	if got := c.Current(); got != 32*kiB {
		t.Fatalf("Current() = %d, want %d", got, 32*kiB)
	}
	if got := c.Min(); got != 16*kiB {
		t.Fatalf("Min() = %d, want %d", got, 16*kiB)
	}
	if got := c.Max(); got != 128*kiB {
		t.Fatalf("Max() = %d, want %d", got, 128*kiB)
	}
	if got := c.Step(); got != 16*kiB {
		t.Fatalf("Step() = %d, want %d", got, 16*kiB)
	}
}

func TestIncrease(t *testing.T) {
	c := mustNew(t, 16*kiB, 128*kiB, 32*kiB, 16*kiB)
	if got := c.Increase(); got != 48*kiB {
		t.Fatalf("Increase() = %d, want %d", got, 48*kiB)
	}
	if got := c.Increase(); got != 64*kiB {
		t.Fatalf("Increase() = %d, want %d", got, 64*kiB)
	}
}

func TestDecrease(t *testing.T) {
	c := mustNew(t, 16*kiB, 128*kiB, 64*kiB, 16*kiB)
	if got := c.Decrease(); got != 48*kiB {
		t.Fatalf("Decrease() = %d, want %d", got, 48*kiB)
	}
	if got := c.Decrease(); got != 32*kiB {
		t.Fatalf("Decrease() = %d, want %d", got, 32*kiB)
	}
}

func TestRepeatedIncreaseAtMaximum(t *testing.T) {
	c := mustNew(t, 16*kiB, 64*kiB, 32*kiB, 16*kiB)
	for i := 0; i < 10; i++ {
		c.Increase()
	}
	if got := c.Current(); got != 64*kiB {
		t.Fatalf("Current() = %d, want %d (max)", got, 64*kiB)
	}
}

func TestRepeatedDecreaseAtMinimum(t *testing.T) {
	c := mustNew(t, 16*kiB, 64*kiB, 32*kiB, 16*kiB)
	for i := 0; i < 10; i++ {
		c.Decrease()
	}
	if got := c.Current(); got != 16*kiB {
		t.Fatalf("Current() = %d, want %d (min)", got, 16*kiB)
	}
}

func TestSetClamps(t *testing.T) {
	c := mustNew(t, 16*kiB, 64*kiB, 32*kiB, 8*kiB)

	if got := c.Set(1); got != 16*kiB {
		t.Fatalf("Set(below min) = %d, want %d", got, 16*kiB)
	}
	if got := c.Set(1 << 20); got != 64*kiB {
		t.Fatalf("Set(above max) = %d, want %d", got, 64*kiB)
	}
	if got := c.Set(40 * kiB); got != 40*kiB {
		t.Fatalf("Set(in range) = %d, want %d", got, 40*kiB)
	}
}

func TestMinEqualsMax(t *testing.T) {
	c := mustNew(t, 32*kiB, 32*kiB, 32*kiB, 8*kiB)
	if got := c.Increase(); got != 32*kiB {
		t.Fatalf("Increase() = %d, want %d", got, 32*kiB)
	}
	if got := c.Decrease(); got != 32*kiB {
		t.Fatalf("Decrease() = %d, want %d", got, 32*kiB)
	}
	if got := c.Set(100); got != 32*kiB {
		t.Fatalf("Set(100) = %d, want %d", got, 32*kiB)
	}
}

func TestInvalidConfig(t *testing.T) {
	cases := []struct {
		name                    string
		min, max, initial, step int64
	}{
		{"zero min", 0, 64, 32, 8},
		{"negative min", -1, 64, 32, 8},
		{"max below min", 32, 16, 32, 8},
		{"initial below min", 16, 64, 8, 8},
		{"initial above max", 16, 64, 128, 8},
		{"zero step", 16, 64, 32, 0},
		{"negative step", 16, 64, 32, -8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.min, tc.max, tc.initial, tc.step); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New(%d, %d, %d, %d) = %v, want ErrInvalidConfig", tc.min, tc.max, tc.initial, tc.step, err)
			}
		})
	}
}

func TestConfiguredStepSize(t *testing.T) {
	c := mustNew(t, 8*kiB, 64*kiB, 16*kiB, 8*kiB)
	if got := c.Increase(); got != 24*kiB {
		t.Fatalf("Increase() = %d, want %d", got, 24*kiB)
	}
	if got := c.Decrease(); got != 16*kiB {
		t.Fatalf("Decrease() = %d, want %d", got, 16*kiB)
	}

	big := mustNew(t, 8*kiB, 1<<20, 64*kiB, 256*kiB)
	if got := big.Increase(); got != 320*kiB {
		t.Fatalf("Increase() = %d, want %d", got, 320*kiB)
	}
	if got := big.Decrease(); got != 64*kiB {
		t.Fatalf("Decrease() = %d, want %d", got, 64*kiB)
	}
}

// TestSemanticSequence exercises the exact example from the specification:
// min=16KiB, max=128KiB, initial=32KiB, step=16KiB -> 32, 48, 64, 80, ...
func TestSemanticSequence(t *testing.T) {
	c := mustNew(t, 16*kiB, 128*kiB, 32*kiB, 16*kiB)

	want := []int64{48 * kiB, 64 * kiB, 80 * kiB, 96 * kiB, 112 * kiB, 128 * kiB}
	for i, w := range want {
		if got := c.Increase(); got != w {
			t.Fatalf("Increase #%d = %d, want %d", i+1, got, w)
		}
	}
	// Never exceeds max.
	for i := 0; i < 5; i++ {
		if got := c.Increase(); got != 128*kiB {
			t.Fatalf("Increase past max = %d, want %d", got, 128*kiB)
		}
	}
	// Symmetric decrease back to min.
	for i := 0; i < 10; i++ {
		c.Decrease()
	}
	if got := c.Current(); got != 16*kiB {
		t.Fatalf("Current() after decreases = %d, want %d", got, 16*kiB)
	}
}

func TestConcurrentAccess(t *testing.T) {
	const (
		min, max, initial, step = 1, 1024, 16, 4
		goroutines              = 32
		ops                     = 500
	)
	c := mustNew(t, min, max, initial, step)

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
					c.Set(int64(i % (max + 5)))
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
