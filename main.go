package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"strconv"
	// "strings" // No longer needed directly by main after moving getID/getEvent
	"time"

	"github.com/blinkinglight/go-experiment-eventsourcing/pkg/events" // Import the new events package
	"github.com/blinkinglight/go-experiment-eventsourcing/pkg/tools"
	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	datastar "github.com/starfederation/datastar/sdk/go"
	"database/sql"
	// Removed duplicate imports below
)

func main() {
	db, err := InitDB("read_model.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	getPort := func() int {
		for {
			nl, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				continue
			}
			defer nl.Close()
			addr := nl.Addr().(*net.TCPAddr)
			return addr.Port
		}
	}

	opts := &server.Options{
		ServerName: "embedded-nats-server",
		JetStream:  true,
		StoreDir:   "./data",
		Port:       getPort(),
	}
	ns := server.New(opts)
	log.Printf("Starting nats server on port %d", opts.Port)
	ns.Start()
	defer ns.Shutdown()
	if !ns.ReadyForConnections(5 * time.Second) {
		panic("nats server not ready")
	}

	_ = ctx
	log.Printf("NATS server started on %s", ns.ClientURL())

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		panic(err)
	}
	defer nc.Drain()
	defer nc.Close()

	id := "z23ntlMT7yPIvFy4FGQ7or"
	js, _ := nc.JetStream()

	// Rebuild the read model on startup
	log.Println("Attempting to rebuild read model...")
	if err := RebuildReadModel(js, db, "users", "users.>"); err != nil {
		log.Fatalf("Failed to rebuild read model: %v", err)
	} else {
		log.Println("Read model rebuilt successfully.")
	}

	r := chi.NewMux()

	r.Post("/post", func(w http.ResponseWriter, r *http.Request) {
		nc.Publish("commands.post", nil)
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		Main().Render(r.Context(), w)
	})

	r.Post("/error", func(w http.ResponseWriter, r *http.Request) {
		nc.Publish("errors", []byte("random error"))
	})

	r.Get("/stream", func(w http.ResponseWriter, r *http.Request) {
		sse := datastar.NewSSE(w, r)
		var pipe = make(chan *nats.Msg, 128)
		var errPipe = make(chan *nats.Msg, 128)
		sub, _ := js.ChanSubscribe("users."+id+".>", pipe, nats.DeliverNew())
		defer sub.Unsubscribe()
		errSub, _ := nc.ChanSubscribe("errors", errPipe)
		defer errSub.Unsubscribe()

		var state = replay(r.Context(), nc, "users", id, replayFn)
		sse.MergeFragmentTempl(Part(state))

		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-errPipe:
				state.Errors = append(state.Errors, string(msg.Data))
				sse.MergeFragmentTempl(Part(state))
			case msg := <-pipe:
				switch events.GetEvent(msg.Subject) { // Use events.GetEvent
				case "created":
					user, _ := tools.Unmarshal[events.UserCreated](msg.Data) // Use events.UserCreated
					state.Name = user.Name
					state.Lastname = user.Lastname
				case "address":
					address, _ := tools.Unmarshal[events.AddressUpdated](msg.Data) // Use events.AddressUpdated
					state.Address = address.Address
					state.UpdatedAt = address.CreatedAt
				}
				sse.MergeFragmentTempl(Part(state))
			}
		}
	})

	js.AddStream(&nats.StreamConfig{
		Name:     "users",
		Subjects: []string{"users.>"},
	})

	_ = js
	js.PurgeStream("users")
	js.Publish(fmt.Sprintf("users.%s.created", id), []byte(`{"name":"John", "lastname":"Doe", "created_at":"2021-09-01"}`))
	js.Publish(fmt.Sprintf("users.%s.address", id), []byte(`{"address":"123 Main St", "created_at":"2021-10-01"}`))
	js.Publish(fmt.Sprintf("users.%s.addressv2", id), []byte(`{"address":"v2 address", "created_at":"2021-11-01"}`))
	js.Publish(fmt.Sprintf("users.%s.somethingnotimplementedyet", id), []byte(`{"other":"not implemented yet", "created_at":"2021-12-01"}`))
	js.Publish(fmt.Sprintf("users.%s.addressv3", id), []byte(`{"address":"v3 address", "city":"v3 city", "country":"v3 country", "created_at":"2021-12-01"}`))

	sb, _ := nc.Subscribe("commands.>", func(msg *nats.Msg) {
		num, _ := rand.Int(rand.Reader, big.NewInt(1000))
		number := strconv.Itoa(int(num.Int64()))
		today := time.Now().Format("2006-01-02")
		js.Publish(fmt.Sprintf("users.%s.address", id), []byte(`{"address":"`+number+`", "created_at":"`+today+`"}`))
	})
	defer sb.Unsubscribe()

	time.Sleep(1 * time.Second)

	state := replay(ctx, nc, "users", id, replayFn)
	// persist state to db
	// we could use few events here for read model
	js.Subscribe("users.>", func(msg *nats.Msg) {
		log.Printf("Persisting read-model - Received event %s with payload %s", events.GetEvent(msg.Subject), msg.Data)

		eventID := events.GetID(msg.Subject) // Use events.GetID

		originalEventType := events.GetEvent(msg.Subject)
		eventData := msg.Data

		switch originalEventType {
		case "created":
			user, err := tools.Unmarshal[events.UserCreated](eventData)
			if err != nil {
				log.Printf("NATS Sub: Error unmarshalling UserCreated for ID %s: %v", eventID, err)
				msg.Ack()
				return
			}
			if err := HandleUserCreatedEvent(db, eventID, user.Name, user.Lastname, user.CreatedAt); err != nil {
				// Error is already logged in HandleUserCreatedEvent, log context here
				log.Printf("NATS Sub: Error handling UserCreatedEvent for ID %s: %v", eventID, err)
			}
		case "address", "addressv2", "addressv3":
			v3Payload, err := events.UpcastToAddressUpdatedV3(eventData, originalEventType)
			if err != nil {
				log.Printf("NATS Sub: Error upcasting event type '%s' for ID %s: %v", originalEventType, eventID, err)
				msg.Ack()
				return
			}
			if err := HandleAddressUpdatedEvent(db, eventID, v3Payload); err != nil {
				// Error is already logged in HandleAddressUpdatedEvent, log context here
				log.Printf("NATS Sub: Error handling AddressUpdatedEvent (original type: '%s') for ID %s: %v", originalEventType, eventID, err)
			}
		default:
			log.Printf("NATS Sub: Unknown event type '%s' for ID %s", originalEventType, eventID)
		}
		msg.Ack()
		// maybe tell FE to update
		nc.Publish("state."+eventID, nil)
	}, nats.AckExplicit(), nats.Durable("read-model"), nats.ManualAck())

	log.Printf("Final state %+v", state)

	log.Fatal(http.ListenAndServe(":9999", r))
}

func replayFn(ctx context.Context, id string, msgs <-chan *nats.Msg) (state State) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			eventSubject := msg.Subject // Store for repeated use
			eventData := msg.Data
			originalEventType := events.GetEvent(eventSubject)

			switch originalEventType {
			case "created":
				user, err := tools.Unmarshal[events.UserCreated](eventData)
				if err != nil {
					log.Printf("replayFn: Error unmarshalling UserCreated: %v", err)
					continue // Skip this event
				}
				state.Name = user.Name
				state.Lastname = user.Lastname
				state.Changes = append(state.Changes, "created at "+user.CreatedAt)
			case "address", "addressv2", "addressv3":
				v3Payload, err := events.UpcastToAddressUpdatedV3(eventData, originalEventType)
				if err != nil {
					log.Printf("replayFn: Error upcasting event type '%s': %v", originalEventType, err)
					continue // Skip this event
				}
				// Construct address string, handling potential empty city/country
				fullAddress := v3Payload.AddressUpdated.Address
				if v3Payload.City != "" {
					fullAddress += ", " + v3Payload.City
				}
				if v3Payload.Country != "" {
					fullAddress += ", " + v3Payload.Country
				}
				state.Address = fullAddress
				state.UpdatedAt = v3Payload.CreatedAt // Use the CreatedAt from the V3 payload
				state.Changes = append(state.Changes, "address updated at "+v3Payload.CreatedAt)
			default:
				log.Printf("replayFn: Unknown event type '%s' with payload %s", originalEventType, eventData)
			}
		}
	}

}

type onReqFn[T any] func(ctx context.Context, id string, msgs <-chan *nats.Msg) T

func replay[T any](ctx context.Context, nc *nats.Conn, domain, id string, fn onReqFn[T]) T {
	js, _ := nc.JetStream()
	lctx, lcfn := context.WithCancel(ctx)
	msgs := make(chan *nats.Msg, 128)
	messages := make(chan *nats.Msg, 128)

	sub, _ := js.ChanSubscribe(fmt.Sprintf("%s.%s.>", domain, id), msgs, nats.AckExplicit(), nats.DeliverAll())
	defer close(msgs)
	defer close(messages)
	defer sub.Unsubscribe()
	delay := 100 * time.Millisecond

	go func() {
		waiter := time.NewTimer(delay)
		for {
			select {
			case <-ctx.Done():
				lcfn()
				return
			case <-waiter.C:
				lcfn()
				return
			case msg := <-msgs:
				waiter.Reset(delay)
				messages <- msg
				msg.Ack()
			}
		}
	}()
	return fn(lctx, id, messages)
}

// getEvent and getID are now in pkg/events/utils.go
type State struct {
	ID       string
	Name     string
	Lastname string
	Address  string

	UpdatedAt string
	Errors    []string
	Changes   []string
}

// Event type definitions (UserCreated, AddressUpdated, AddressUpdatedV3)
// were confirmed to be moved to pkg/events/types.go in a previous step.
// This comment block in main.go is a placeholder for where they used to be.
// No actual code lines for these types should exist below this point in main.go.
