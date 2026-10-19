package recorder

import (
	"encoding/json"
	"fmt"
	"opcua-ai-studio/internal/device"
)

type packetMetadata struct {
	Version int                  `json:"version"`
	Family  string               `json:"family,omitempty"`
	Targets []device.Address     `json:"targets,omitempty"`
	Results []device.EventResult `json:"results,omitempty"`
	Legacy  *device.LegacyFields `json:"legacy,omitempty"`
	Total   int                  `json:"total,omitempty"`
}

func storePacketMetadata(e device.Event) (string, error) {
	b, err := json.Marshal(packetMetadata{1, e.Family, e.Targets, e.Results, e.Legacy, e.Total})
	return string(b), err
}
func restorePacketMetadata(e *device.Event, text string, legacy device.LegacyFields) error {
	if text == "" {
		if e.Protocol != device.ProtocolOPCUA {
			e.Legacy = &legacy
		}
		return nil
	}
	var m packetMetadata
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return fmt.Errorf("invalid packet metadata: %w", err)
	}
	if m.Version != 1 {
		return fmt.Errorf("unsupported packet metadata version %d", m.Version)
	}
	e.Family, e.Targets, e.Results, e.Legacy, e.Total = m.Family, m.Targets, m.Results, m.Legacy, m.Total
	return nil
}
