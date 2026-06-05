package topic

import (
	"fmt"
	"strings"
)

type Parsed struct {
	SiteID     string
	DeviceType string
	DeviceID   string
	Kind       string
}

func Telemetry(siteID, deviceType, deviceID string) string {
	return fmt.Sprintf("vpp/%s/%s/%s/telemetry", siteID, deviceType, deviceID)
}

func Command(siteID, deviceType, deviceID string) string {
	return fmt.Sprintf("vpp/%s/%s/%s/command", siteID, deviceType, deviceID)
}

func Wildcard(siteID string) string {
	return fmt.Sprintf("vpp/%s/+/+/+", siteID)
}

func Parse(t string) (Parsed, bool) {
	parts := strings.Split(t, "/")
	if len(parts) < 5 || parts[0] != "vpp" {
		return Parsed{}, false
	}
	p := Parsed{
		SiteID:     parts[1],
		DeviceType: parts[2],
		DeviceID:   parts[3],
		Kind:       parts[4],
	}
	if len(parts) == 6 {
		p.Kind = parts[4] + "/" + parts[5]
	}
	return p, true
}
