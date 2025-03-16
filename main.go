package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/blinkinglight/go-experiment-eventsourcing/pkg/tools"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func main() {

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

	time.Sleep(1 * time.Second)

	state := replay(ctx, nc, "users", id, func(ctx context.Context, id string, msgs <-chan *nats.Msg) (state State) {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				switch getEvent(msg.Subject) {
				case "created":
					user, _ := tools.Unmarshal[UserCreated](msg.Data)
					state.Name = user.Name
					state.Lastname = user.Lastname
					state.Changes = append(state.Changes, "created at "+user.CreatedAt)
				case "address":
					address, _ := tools.Unmarshal[AddressUpdated](msg.Data)
					state.Address = address.Address
					state.Changes = append(state.Changes, "address updated at"+address.CreatedAt)
				case "addressv2":
					address, _ := tools.Unmarshal[AddressUpdated](msg.Data)
					state.Address = address.Address
					state.Changes = append(state.Changes, "address updated at"+address.CreatedAt)
				case "addressv3":
					address, _ := tools.Unmarshal[AddressUpdatedV3](msg.Data)
					state.Address = address.Address + ", " + address.City + ", " + address.Country
					state.Changes = append(state.Changes, "address updated at"+address.CreatedAt)
				default:
					log.Printf("Unknown event: %s with payload %s", getEvent(msg.Subject), msg.Data)
				}
			}
		}
	})

	log.Printf("Final state %+v", state)

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

func getEvent(subject string) string {
	parts := strings.SplitN(subject, ".", 3)
	return parts[len(parts)-1]
}

type State struct {
	ID       string
	Name     string
	Lastname string
	Address  string

	Changes []string
}

type UserCreated struct {
	Name      string `json:"name"`
	Lastname  string `json:"lastname"`
	CreatedAt string `json:"created_at"`
}

type AddressUpdated struct {
	Address   string `json:"address"`
	CreatedAt string `json:"created_at"`
}

type AddressUpdatedV3 struct {
	*AddressUpdated
	City      string `json:"city"`
	Country   string `json:"country"`
	CreatedAt string `json:"created_at"`
}
