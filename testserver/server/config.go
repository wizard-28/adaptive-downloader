package server

import (
	"fmt"
)

const (
	DefaultPort                    = 8080
	DefaultFileSize                = "100MiB"
	DefaultConcurrencyThreshold    = 4
	DefaultBelowThresholdBandwidth = "30MiB"
	DefaultAboveThresholdBandwidth = "10MiB"
)

type Config struct {
	Port     int
	FileSize int64

	ConcurrencyThreshold    int
	BelowThresholdBandwidth int64
	AboveThresholdBandwidth int64
}

func (c Config) Validate() error {
	if c.Port < 1024 || c.Port > 65535 {
		return fmt.Errorf("port number must be valid and non-priviledged, got %d", c.Port)
	}

	if c.FileSize <= 0 {
		return fmt.Errorf(
			"file size must be greater than 0, got %d",
			c.FileSize,
		)
	}

	if c.AboveThresholdBandwidth > 0 && c.AboveThresholdBandwidth > c.BelowThresholdBandwidth {
		return fmt.Errorf("bandwidth above threhold must be > 0 & > bandwidth below threhold")
	}

	return nil
}
