package events

import (
	"encoding/json"
	"fmt"
	// Assuming 'your_project' is the root of the go module, e.g., 'github.com/blinkinglight/go-experiment-eventsourcing'
	// Adjust the import path according to your actual module name.
	"github.com/blinkinglight/go-experiment-eventsourcing/pkg/tools" 
)

// AddressUpdatedV3 is expected to be defined in pkg/events/types.go
// UserCreated is expected to be defined in pkg/events/types.go
// AddressUpdated is expected to be defined in pkg/events/types.go

// UpcastToAddressUpdatedV3 takes raw event data and the original event type,
// and attempts to convert it into an AddressUpdatedV3 event.
// It handles different versions of address events ("address", "addressv2", "addressv3").
func UpcastToAddressUpdatedV3(eventData []byte, originalEventType string) (AddressUpdatedV3, error) {
	switch originalEventType {
	case "addressv3":
		var v3 AddressUpdatedV3
		// tools.Unmarshal is a generic function, its direct usage might look like:
		// v3, err := tools.Unmarshal[AddressUpdatedV3](eventData)
		// However, if tools.Unmarshal is just a wrapper around json.Unmarshal like:
		// func Unmarshal[T any](data []byte, v *T) error { return json.Unmarshal(data, v) }
		// then the call below is more appropriate if Unmarshal expects a pointer.
		// The prompt showed `tools.Unmarshal[T any](data []byte) (T, error)`
		// which means it returns the value, so `v3, err := tools.Unmarshal[AddressUpdatedV3](eventData)` would be the way.
		// tools.Unmarshal returns the value and error.
		v3, err := tools.Unmarshal[AddressUpdatedV3](eventData)
		if err != nil {
			return AddressUpdatedV3{}, fmt.Errorf("error unmarshalling AddressUpdatedV3 for event type %s: %w", originalEventType, err)
		}
		// The AddressUpdatedV3.CreatedAt (outer) should be correctly populated by Unmarshal
		// if 'created_at' is a top-level field in the JSON.
		return v3, nil

	case "address", "addressv2":
		v_old, err := tools.Unmarshal[AddressUpdated](eventData) // Use tools.Unmarshal consistent with the clarified signature
		if err != nil {
			return AddressUpdatedV3{}, fmt.Errorf("error unmarshalling %s into events.AddressUpdated: %w", originalEventType, err)
		}

		return AddressUpdatedV3{
			AddressUpdated: &AddressUpdated{ // Populate the embedded struct
				Address:   v_old.Address,
				CreatedAt: v_old.CreatedAt, // This will go into the embedded AddressUpdated.CreatedAt
			},
			City:      "", // Default to empty string for older versions
			Country:   "", // Default to empty string for older versions
			CreatedAt: v_old.CreatedAt, // Explicitly set the outer CreatedAt for the V3 struct from the old version's CreatedAt
		}, nil

	default:
		return AddressUpdatedV3{}, fmt.Errorf("unknown event type for upcasting to AddressUpdatedV3: %s", originalEventType)
	}
}
