package events

import (
	"strings"
)

// GetEvent extracts the event type from a NATS subject string.
// Example: "users.z23ntlMT7yPIvFy4FGQ7or.created" -> "created"
func GetEvent(subject string) string {
	parts := strings.SplitN(subject, ".", 3)
	if len(parts) < 3 {
		return "" // Or handle error appropriately
	}
	return parts[len(parts)-1]
}

// GetID extracts the ID from a NATS subject string.
// Example: "users.z23ntlMT7yPIvFy4FGQ7or.created" -> "z23ntlMT7yPIvFy4FGQ7or"
func GetID(subject string) string {
	parts := strings.SplitN(subject, ".", 3)
	if len(parts) < 2 {
		return "" // Or handle error appropriately
	}
	return parts[1]
}
