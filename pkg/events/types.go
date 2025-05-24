package events

// UserCreated defines the structure for user creation events.
type UserCreated struct {
	Name      string `json:"name"`
	Lastname  string `json:"lastname"`
	CreatedAt string `json:"created_at"`
}

// AddressUpdated defines the structure for address update events.
type AddressUpdated struct {
	Address   string `json:"address"`
	CreatedAt string `json:"created_at"`
}

// AddressUpdatedV3 defines the structure for V3 address update events,
// including city and country.
type AddressUpdatedV3 struct {
	*AddressUpdated // Embedded to include Address and CreatedAt
	City            string `json:"city"`
	Country         string `json:"country"`
	// CreatedAt is intentionally repeated here to override the embedded one if necessary,
	// or ensure it's correctly populated by tools.Unmarshal for this specific type.
	// If AddressUpdated.CreatedAt is always the one to use, this can be removed.
	CreatedAt string `json:"created_at"`
}
