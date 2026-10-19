package service

import (
	"fmt"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"time"
)

type Config struct {
	Target string
	opcua.Options
}
type Point struct {
	Address string
	Type    device.DataType
	Count   int
}
type WriteRequest struct {
	Point      Point
	Value      string
	Generation uint64
}

func DefaultConfig() Config { return Config{Options: opcua.DefaultOptions()} }
func (cfg Config) validate() (opcua.Options, error) {
	if err := opcua.ValidateEndpoint(cfg.Target); err != nil {
		return cfg.Options, err
	}
	return cfg.Options, cfg.Options.Validate()
}
func resolve(points []Point) ([]device.Address, error) {
	if len(points) < 1 || len(points) > maxPoints {
		return nil, fmt.Errorf("请输入 1..%d 个节点", maxPoints)
	}
	aa := make([]device.Address, len(points))
	for i, p := range points {
		a, err := device.ParseAddress(p.Address, p.Type, p.Count)
		if err != nil {
			return nil, err
		}
		aa[i] = a
	}
	return aa, nil
}

var DefaultPollInterval = time.Second
