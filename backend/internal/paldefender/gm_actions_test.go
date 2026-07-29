package paldefender

import (
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"palpanel/internal/palconfig"
)

func TestTypedRCONPlayerManagementCommands(t *testing.T) {
	manager, cleanup := testManager(t)
	defer cleanup()
	prepareGMRESTFixture(t, manager)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := palconfig.Write(manager.cfg.PalWorldSettingsPath(), palconfig.Settings{
		"RCONEnabled": "True", "RCONPort": strconv.Itoa(port), "AdminPassword": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	commands := make(chan string, 4)
	go serveTestRCON(listener, commands)

	if _, err := manager.RCONRemoveItems(t.Context(), "steam_1", RemoveItemsRequest{Items: []ItemGrant{{ItemID: "Money", Count: 5}, {ItemID: "Stone", Count: 2}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RCONTeleport(t.Context(), "steam_1", TeleportRequest{Mode: "coordinates", X: pointer(12.5), Y: pointer(-30.0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RCONTeleport(t.Context(), "steam_1", TeleportRequest{Mode: "player", TargetPlayer: "gdk_2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RCONReleasePal(t.Context(), "steam_1", ReleasePalRequest{PalID: "Anubis", Level: pointer(50), Gender: "male", Rank: pointer(4), Lucky: pointer(true)}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"/delitems steam_1 Money:5 Stone:2",
		"/tp steam_1 12.5 -30",
		"/tp steam_1 gdk_2",
		"/deletepals steam_1 ID Anubis Level=50 Gender male Rank=4 Lucky true Limit 1",
	} {
		select {
		case got := <-commands:
			if got != want {
				t.Fatalf("command = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("command %q was not received", want)
		}
	}
}

func TestTypedRCONPlayerManagementValidation(t *testing.T) {
	manager, cleanup := testManager(t)
	defer cleanup()
	tests := []struct {
		name string
		run  func() error
	}{
		{"item injection", func() error {
			_, err := manager.RCONRemoveItems(t.Context(), "steam_1", RemoveItemsRequest{Items: []ItemGrant{{ItemID: "Money\n/ban", Count: 1}}})
			return err
		}},
		{"duplicate item", func() error {
			_, err := manager.RCONRemoveItems(t.Context(), "steam_1", RemoveItemsRequest{Items: []ItemGrant{{ItemID: "Money", Count: 1}, {ItemID: "money", Count: 2}}})
			return err
		}},
		{"missing coordinates", func() error {
			_, err := manager.RCONTeleport(t.Context(), "steam_1", TeleportRequest{Mode: "coordinates", X: pointer(1.0)})
			return err
		}},
		{"same target", func() error {
			_, err := manager.RCONTeleport(t.Context(), "steam_1", TeleportRequest{Mode: "player", TargetPlayer: "steam_1"})
			return err
		}},
		{"pal injection", func() error {
			_, err := manager.RCONReleasePal(t.Context(), "steam_1", ReleasePalRequest{PalID: "Anubis Limit 99"})
			return err
		}},
		{"invalid gender", func() error {
			_, err := manager.RCONReleasePal(t.Context(), "steam_1", ReleasePalRequest{PalID: "Anubis", Gender: "any"})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestTypedRCONRetriesWithoutSlashForCompatibleServers(t *testing.T) {
	manager, cleanup := testManager(t)
	defer cleanup()
	prepareGMRESTFixture(t, manager)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := palconfig.Write(manager.cfg.PalWorldSettingsPath(), palconfig.Settings{
		"RCONEnabled": "True", "RCONPort": strconv.Itoa(port), "AdminPassword": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	commands := make(chan string, 2)
	go serveTestRCONResponses(listener, commands, "Unknown command", "OK")

	result, err := manager.RCONTeleport(t.Context(), "steam_1", TeleportRequest{Mode: "coordinates", X: pointer(1.0), Y: pointer(1.0)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Command != "tp steam_1 1 1" || result.Output != "OK" {
		t.Fatalf("result = %#v", result)
	}
	for _, want := range []string{"/tp steam_1 1 1", "tp steam_1 1 1"} {
		select {
		case got := <-commands:
			if got != want {
				t.Fatalf("command = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("command %q was not received", want)
		}
	}
}

func TestTypedRCONRejectsUnknownCommandOutput(t *testing.T) {
	manager, cleanup := testManager(t)
	defer cleanup()
	prepareGMRESTFixture(t, manager)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := palconfig.Write(manager.cfg.PalWorldSettingsPath(), palconfig.Settings{
		"RCONEnabled": "True", "RCONPort": strconv.Itoa(port), "AdminPassword": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	commands := make(chan string, 2)
	go serveTestRCONResponses(listener, commands, "Unknown command", "Unknown command")

	if _, err := manager.RCONTeleport(t.Context(), "steam_1", TeleportRequest{Mode: "coordinates", X: pointer(1.0), Y: pointer(1.0)}); !errors.Is(err, ErrRCONCommandRejected) {
		t.Fatalf("error = %v", err)
	}
}

func serveTestRCON(listener net.Listener, commands chan<- string) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func(conn net.Conn) {
			defer conn.Close()
			auth, err := readRCONPacket(conn)
			if err != nil || auth.Type != rconPacketAuth || auth.Body != "secret" {
				return
			}
			_ = writeRCONPacket(conn, rconPacket{ID: auth.ID, Type: rconPacketAuthReply})
			command, err := readRCONPacket(conn)
			if err != nil {
				return
			}
			commands <- command.Body
			_ = writeRCONPacket(conn, rconPacket{ID: command.ID, Type: rconPacketExecReply, Body: "OK"})
			time.Sleep(250 * time.Millisecond)
		}(conn)
	}
}

func serveTestRCONResponses(listener net.Listener, commands chan<- string, responses ...string) {
	for _, response := range responses {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		auth, err := readRCONPacket(conn)
		if err != nil || auth.Type != rconPacketAuth || auth.Body != "secret" {
			_ = conn.Close()
			return
		}
		_ = writeRCONPacket(conn, rconPacket{ID: auth.ID, Type: rconPacketAuthReply})
		command, err := readRCONPacket(conn)
		if err != nil {
			_ = conn.Close()
			return
		}
		commands <- command.Body
		_ = writeRCONPacket(conn, rconPacket{ID: command.ID, Type: rconPacketExecReply, Body: response})
		time.Sleep(250 * time.Millisecond)
		_ = conn.Close()
	}
}

func pointer[T any](value T) *T { return &value }
