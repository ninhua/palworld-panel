package paldefender

import (
	"errors"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"palpanel/internal/palconfig"
)

func TestTypedRCONWhitelistAdminAndCatalogCommands(t *testing.T) {
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
	go func() {
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
				body := `{"commands":["Technology_ElecBaton","Technology_GrapplingGun"]}`
				if command.Body == "/whitelist_get" {
					body = `["steam_1","gdk_2"]`
				}
				_ = writeRCONPacket(conn, rconPacket{ID: command.ID, Type: rconPacketExecReply, Body: body})
				time.Sleep(250 * time.Millisecond)
			}(conn)
		}
	}()

	whitelist, err := manager.RCONWhitelist(t.Context())
	if err != nil || len(whitelist.Entries) != 2 || whitelist.Entries[0] != "gdk_2" {
		t.Fatalf("RCONWhitelist = %#v, %v", whitelist, err)
	}
	if _, err := manager.RCONWhitelistAdd(t.Context(), "steam_3"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RCONSetAdmin(t.Context(), "steam_3"); err != nil {
		t.Fatal(err)
	}
	catalog, err := manager.RCONTechnologyCatalog(t.Context())
	if err != nil || len(catalog.Entries) != 2 {
		t.Fatalf("RCONTechnologyCatalog = %#v, %v", catalog, err)
	}
	for _, want := range []string{"/whitelist_get", "/whitelist_add steam_3", "/setadmin steam_3", "/gettechids"} {
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

func TestRCONAcceptsPalDefenderGenericResponseID(t *testing.T) {
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
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		auth, err := readRCONPacket(conn)
		if err != nil {
			return
		}
		_ = writeRCONPacket(conn, rconPacket{ID: auth.ID, Type: rconPacketAuthReply})
		command, err := readRCONPacket(conn)
		if err != nil || command.Body != "/getrconcmds" {
			return
		}
		_ = writeRCONPacket(conn, rconPacket{
			ID:   rconGenericResponseID,
			Type: rconPacketExecReply,
			Body: "getrconcmds:0;tp:1;delitems:2;",
		})
	}()

	result, err := manager.RCONCommands(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "getrconcmds:0;tp:1;delitems:2;" {
		t.Fatalf("RCONCommands output = %q", result.Output)
	}
}

func TestRCONCommandResponseStillRequiresExecutionReplyType(t *testing.T) {
	if isRCONCommandResponse(rconPacket{ID: rconGenericResponseID, Type: rconPacketAuthReply}, rconCommandRequestID) {
		t.Fatal("authentication reply must not be accepted as a command response")
	}
	if !isRCONCommandResponse(rconPacket{ID: rconCommandRequestID, Type: rconPacketExecReply}, rconCommandRequestID) {
		t.Fatal("matching execution reply should be accepted")
	}
}

func TestAccessSettingsValidationAndPersistence(t *testing.T) {
	manager, cleanup := testManager(t)
	defer cleanup()
	settings, err := manager.WriteAccessSettings(AccessSettingsUpdate{
		UseWhitelist: true, WhitelistMessage: "Members only",
		UseAdminWhitelist: true, AdminAutoLogin: true,
		AdminIPs: []string{"127.0.0.1", "192.168.*.*", "127.0.0.1"},
	})
	if err != nil || !settings.UseWhitelist || !settings.UseAdminWhitelist || len(settings.AdminIPs) != 2 || !settings.ReloadRequired {
		t.Fatalf("WriteAccessSettings = %#v, %v", settings, err)
	}
	stored, err := manager.ReadAccessSettings()
	if err != nil || stored.WhitelistMessage != "Members only" || len(stored.AdminIPs) != 2 {
		t.Fatalf("ReadAccessSettings = %#v, %v", stored, err)
	}
	if _, err := manager.WriteAccessSettings(AccessSettingsUpdate{UseAdminWhitelist: true}); err == nil {
		t.Fatal("admin whitelist without IPs should fail")
	}
	if _, err := manager.WriteAccessSettings(AccessSettingsUpdate{AdminIPs: []string{"999.1.1.1"}}); err == nil {
		t.Fatal("invalid administrator IP should fail")
	}
	if _, err := os.Stat(manager.configPath()); err != nil {
		t.Fatal(err)
	}
}

func TestRCONRejectsLinuxContainerPortMismatch(t *testing.T) {
	manager, cleanup := testManager(t)
	defer cleanup()
	prepareGMRESTFixture(t, manager)
	manager.cfg.RCONPort = 25575
	if err := manager.store.SetKV(t.Context(), kvRuntimeMode, runtimeWineDocker); err != nil {
		t.Fatal(err)
	}
	if err := palconfig.Write(manager.cfg.PalWorldSettingsPath(), palconfig.Settings{
		"RCONEnabled": "True", "RCONPort": "25570", "AdminPassword": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := manager.RCONWhitelist(t.Context())
	if !errors.Is(err, ErrRCONUnavailable) {
		t.Fatalf("error = %v", err)
	}
}
