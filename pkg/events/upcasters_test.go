package events

import (
	"encoding/json"
	"strings"
	"testing"

	// Assuming 'your_project' is the root of the go module.
	// Adjust the import path according to your actual module name.
	// For this project, it is "github.com/blinkinglight/go-experiment-eventsourcing"
	// So, the types package would be "github.com/blinkinglight/go-experiment-eventsourcing/pkg/events"
	// However, since this test file is *in* pkg/events, we can refer to its own package types directly.
	// The tools package needs the full path.
	_ "github.com/blinkinglight/go-experiment-eventsourcing/pkg/tools" // Imported for completeness, though upcaster itself uses it.

	"github.com/stretchr/testify/assert"
)

// TestUpcastToAddressUpdatedV3 will be implemented here.
// The AddressUpdatedV3 struct definition is in types.go (in this same package).
// type AddressUpdatedV3 struct {
// 	*AddressUpdated // Embedded
// 	City            string `json:"city"`
// 	Country         string `json:"country"`
// 	CreatedAt       string `json:"created_at"` // This shadows AddressUpdated.CreatedAt for JSON unmarshalling
// }
// type AddressUpdated struct {
// 	Address   string `json:"address"`
// 	CreatedAt string `json:"created_at"`
// }
//
// tools.Unmarshal is assumed to be:
// func Unmarshal[T any](data []byte) (T, error)
// which internally uses json.Unmarshal.
// For embedded structs and shadowed fields like CreatedAt, json.Unmarshal behaves as follows:
// - If 'created_at' is in the JSON, it will fill AddressUpdatedV3.CreatedAt.
// - If 'address' is in the JSON, it will fill AddressUpdatedV3.AddressUpdated.Address.
// This is standard behavior for json.Unmarshal with embedded structs.
// The upcaster function relies on this.
// The test case for "addressv3" should use a flat JSON structure.
/*
Example AddressUpdatedV3 JSON:
{
	"address": "789 New Ave",
	"city": "New City",
	"country": "New Country",
	"created_at": "2022-03-03"
}
When unmarshalled into AddressUpdatedV3,
- AddressUpdatedV3.AddressUpdated.Address will be "789 New Ave"
- AddressUpdatedV3.City will be "New City"
- AddressUpdatedV3.Country will be "New Country"
- AddressUpdatedV3.CreatedAt will be "2022-03-03" (outer CreatedAt takes precedence for JSON)
- AddressUpdatedV3.AddressUpdated.CreatedAt will be empty unless 'created_at' was also nested,
  or if tools.Unmarshal or the upcaster specifically handles setting it.
  The current upcaster for "addressv3" case just returns the unmarshalled struct.
  For "address" and "addressv2", it explicitly sets both embedded and outer CreatedAt.
  This is fine as AddressUpdatedV3.CreatedAt is the authoritative one.
*/

func TestUpcastToAddressUpdatedV3(t *testing.T) {
	tests := []struct {
		name                       string
		eventData                  []byte
		originalEventType          string
		expectedAddress            string
		expectedCity               string
		expectedCountry            string
		expectedCreatedAt          string
		expectError                bool
		expectedErrorMessageContains string
	}{
		{
			name:              "Valid 'address' event",
			eventData:         []byte(`{"address":"123 Old St", "created_at":"2020-01-01"}`),
			originalEventType: "address",
			expectedAddress:   "123 Old St",
			expectedCity:      "",
			expectedCountry:   "",
			expectedCreatedAt: "2020-01-01",
			expectError:       false,
		},
		{
			name:              "Valid 'addressv2' event",
			eventData:         []byte(`{"address":"456 Second St", "created_at":"2021-02-02"}`),
			originalEventType: "addressv2",
			expectedAddress:   "456 Second St",
			expectedCity:      "",
			expectedCountry:   "",
			expectedCreatedAt: "2021-02-02",
			expectError:       false,
		},
		{
			name:              "Valid 'addressv3' event (flat JSON)",
			eventData:         []byte(`{"address":"789 New Ave", "city":"New City", "country":"New Country", "created_at":"2022-03-03"}`),
			originalEventType: "addressv3",
			expectedAddress:   "789 New Ave",
			expectedCity:      "New City",
			expectedCountry:   "New Country",
			expectedCreatedAt: "2022-03-03",
			expectError:       false,
		},
		{
			name:                       "Malformed JSON for 'address' event (int for address)",
			eventData:                  []byte(`{"address":123, "created_at":"2020-01-01"}`),
			originalEventType:          "address",
			expectError:                true,
			expectedErrorMessageContains: "unmarshalling", // error from tools.Unmarshal/json.Unmarshal
		},
		{
			name:                       "Malformed JSON for 'addressv3' event (array for city)",
			eventData:                  []byte(`{"address":"Test", "city": [], "country":"Test", "created_at":"2022-03-03"}`),
			originalEventType:          "addressv3",
			expectError:                true,
			expectedErrorMessageContains: "unmarshalling", // error from tools.Unmarshal/json.Unmarshal
		},
		{
			name:                       "Unknown event type",
			eventData:                  []byte(`{"data":"some data"}`),
			originalEventType:          "unknown_event_type",
			expectError:                true,
			expectedErrorMessageContains: "unknown event type for upcasting",
		},
		{
			name:                       "Invalid JSON structure for 'address' event",
			eventData:                  []byte(`{"address":"123 Old St", "created_at":"2020-01-01"`), // Missing closing brace
			originalEventType:          "address",
			expectError:                true,
			expectedErrorMessageContains: "unmarshalling",
		},
		{
			name:                       "Invalid JSON structure for 'addressv3' event",
			eventData:                  []byte(`{"address":"789 New Ave", "city":"New City", "country":"New Country", "created_at":"2022-03-03"`), // Missing closing brace
			originalEventType:          "addressv3",
			expectError:                true,
			expectedErrorMessageContains: "unmarshalling",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := UpcastToAddressUpdatedV3(tc.eventData, tc.originalEventType)

			if tc.expectError {
				assert.Error(t, err)
				if tc.expectedErrorMessageContains != "" {
					assert.True(t, strings.Contains(err.Error(), tc.expectedErrorMessageContains), "Error message should contain: %s, but got: %s", tc.expectedErrorMessageContains, err.Error())
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result.AddressUpdated, "AddressUpdated embedded struct should not be nil")
				if result.AddressUpdated != nil { // Guard against nil pointer dereference if previous assert fails
					assert.Equal(t, tc.expectedAddress, result.AddressUpdated.Address)
					// For "address" and "addressv2", the upcaster explicitly sets result.AddressUpdated.CreatedAt
					// For "addressv3", result.AddressUpdated.CreatedAt might be empty if not in source JSON, which is fine.
					// The authoritative CreatedAt is result.CreatedAt (the outer one).
					if tc.originalEventType == "address" || tc.originalEventType == "addressv2" {
						assert.Equal(t, tc.expectedCreatedAt, result.AddressUpdated.CreatedAt, "Embedded CreatedAt for %s", tc.originalEventType)
					}
				}
				assert.Equal(t, tc.expectedCity, result.City)
				assert.Equal(t, tc.expectedCountry, result.Country)
				assert.Equal(t, tc.expectedCreatedAt, result.CreatedAt, "Outer CreatedAt")
			}
		})
	}
}
