package main

import (
	"database/sql"
	"fmt" // Added for error wrapping
	"log" // Added for logging
	"time" // Added for RebuildReadModel timeout

	"github.com/blinkinglight/go-experiment-eventsourcing/pkg/events" // Added for event types
	"github.com/blinkinglight/go-experiment-eventsourcing/pkg/tools"   // Added for Unmarshal
	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go" // Added for nats.JetStreamContext, nats.Msg
)

// InitDB initializes and returns a new SQLite database connection.
// It creates the users_read_model table if it doesn't exist.
func InitDB(filepath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", filepath)
	if err != nil {
		return nil, err
	}

	statement, err := db.Prepare(`
		CREATE TABLE IF NOT EXISTS users_read_model (
			id TEXT PRIMARY KEY,
			name TEXT,
			lastname TEXT,
			address TEXT,
			updated_at TEXT
		);
	`)
	if err != nil {
		return nil, err
	}
	_, err = statement.Exec()
	if err != nil {
		return nil, err
	}
	return db, nil
}

// HandleUserCreatedEvent handles the UserCreated event by inserting or updating
// the user's data in the users_read_model table.
func HandleUserCreatedEvent(db *sql.DB, id, name, lastname, createdAt string) error {
	// Considering using context.Background() for now, as no specific context is passed down.
	// For production systems, a more specific context might be appropriate.
	ctx := context.Background()
	statement, err := db.PrepareContext(ctx, `
		INSERT INTO users_read_model (id, name, lastname, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		name = excluded.name,
		lastname = excluded.lastname,
		updated_at = excluded.updated_at;
	`)
	if err != nil {
		log.Printf("Error preparing statement for UserCreated event for ID %s: %v", id, err)
		return fmt.Errorf("failed to prepare statement for UserCreated event (ID: %s): %w", id, err)
	}
	defer statement.Close()

	_, err = statement.ExecContext(ctx, id, name, lastname, createdAt)
	if err != nil {
		log.Printf("Error executing statement for UserCreated event for ID %s: %v", id, err)
		return fmt.Errorf("failed to execute statement for UserCreated event (ID: %s): %w", id, err)
	}
	return nil
}

// HandleAddressUpdatedEvent handles the AddressUpdated event by updating the user's address.
// If the user does not exist, it creates a new user with the given address.
func HandleAddressUpdatedEvent(db *sql.DB, id, address, updatedAt string) error {
	ctx := context.Background()
	statement, err := db.PrepareContext(ctx, `
		INSERT INTO users_read_model (id, address, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		address = excluded.address,
		updated_at = excluded.updated_at;
	`)
	if err != nil {
		log.Printf("Error preparing statement for AddressUpdated event for ID %s: %v", id, err)
		return fmt.Errorf("failed to prepare statement for AddressUpdated event (ID: %s): %w", id, err)
	}
	defer statement.Close()

	_, err = statement.ExecContext(ctx, id, address, updatedAt)
	if err != nil {
		log.Printf("Error executing statement for AddressUpdated event for ID %s: %v", id, err)
		return fmt.Errorf("failed to execute statement for AddressUpdated event (ID: %s): %w", id, err)
	}
	return nil
}

// RebuildReadModel clears the existing read model and rebuilds it by replaying events from NATS JetStream.
func RebuildReadModel(js nats.JetStreamContext, db *sql.DB, streamName string, subjects string) error {
	log.Printf("Starting read model rebuild for stream '%s', subjects '%s'", streamName, subjects)

	// Clear the users_read_model table
	_, err := db.Exec("DELETE FROM users_read_model;")
	if err != nil {
		log.Printf("Error clearing users_read_model table: %v", err)
		return fmt.Errorf("failed to clear read model table: %w", err)
	}
	log.Println("Successfully cleared users_read_model table.")

	// Channel for receiving messages
	msgChan := make(chan *nats.Msg, 128) // Buffer size can be tuned

	// Subscribe to the stream to get all messages
	// Using DeliverAll, AckExplicit
	sub, err := js.ChanSubscribe(subjects, msgChan, nats.DeliverAll(), nats.AckExplicit(), nats.BindStream(streamName))
	if err != nil {
		log.Printf("Error subscribing to JetStream: %v", err)
		return fmt.Errorf("failed to subscribe to JetStream: %w", err)
	}
	defer sub.Unsubscribe() // Ensure unsubscription on exit

	log.Printf("Successfully subscribed to subjects '%s'", subjects)

	// Message processing loop with timeout
	// Similar to replay logic, wait for a short period after the last message to ensure all available messages are processed.
	timeoutDuration := 2 * time.Second // Adjustable timeout
	timer := time.NewTimer(timeoutDuration)
	defer timer.Stop()

	processingError := false

processLoop:
	for {
		select {
		case msg := <-msgChan:
			timer.Reset(timeoutDuration) // Reset timer on new message

			eventID := events.GetID(msg.Subject)
			eventType := events.GetEvent(msg.Subject)
			eventData := msg.Data

			log.Printf("Rebuilding: Processing event ID '%s', type '%s', data: %s", eventID, eventType, string(eventData))

			var procErr error
			switch eventType {
			case "created":
				var user events.UserCreated
				if err := tools.Unmarshal(eventData, &user); err != nil {
					log.Printf("Rebuild: Error unmarshalling UserCreated for ID %s: %v", eventID, err)
					procErr = err
				} else {
					procErr = HandleUserCreatedEvent(db, eventID, user.Name, user.Lastname, user.CreatedAt)
				}
			case "address", "addressv2":
				var address events.AddressUpdated
				if err := tools.Unmarshal(eventData, &address); err != nil {
					log.Printf("Rebuild: Error unmarshalling AddressUpdated for ID %s: %v", eventID, err)
					procErr = err
				} else {
					procErr = HandleAddressUpdatedEvent(db, eventID, address.Address, address.CreatedAt)
				}
			case "addressv3":
				var address events.AddressUpdatedV3
				if err := tools.Unmarshal(eventData, &address); err != nil {
					log.Printf("Rebuild: Error unmarshalling AddressUpdatedV3 for ID %s: %v", eventID, err)
					procErr = err
				} else {
					fullAddress := address.Address
					if address.City != "" {
						fullAddress += ", " + address.City
					}
					if address.Country != "" {
						fullAddress += ", " + address.Country
					}
					procErr = HandleAddressUpdatedEvent(db, eventID, fullAddress, address.CreatedAt)
				}
			default:
				log.Printf("Rebuild: Unknown event type '%s' for ID %s", eventType, eventID)
			}

			if procErr != nil {
				log.Printf("Rebuild: Error processing event for ID %s, type '%s': %v", eventID, eventType, procErr)
				// Decide if we should stop or continue on error. For now, log and continue.
				// processingError = true // Optionally flag to return an error at the end
			}

			if err := msg.Ack(); err != nil {
				log.Printf("Rebuild: Failed to ACK message for event ID %s: %v", eventID, err)
				// This is a more critical error, might indicate NATS issues.
				processingError = true // Consider stopping or returning an error immediately
				break processLoop
			}

		case <-timer.C:
			log.Println("Rebuild: No more messages received within timeout. Finishing.")
			break processLoop
		}
	}

	if processingError {
		return fmt.Errorf("error occurred during event processing in rebuild")
	}

	log.Println("Read model rebuild completed successfully.")
	return nil
}
