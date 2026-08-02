package boss

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRCONPacketRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	if err := writeRCONPacket(&buffer, 7, rconPacketExecCommand, "/summon Demo"); err != nil {
		t.Fatalf("write packet: %v", err)
	}
	packet, err := readRCONPacket(&buffer)
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if packet.id != 7 || packet.typ != rconPacketExecCommand || packet.body != "/summon Demo" {
		t.Fatalf("unexpected packet: %#v", packet)
	}
}

func TestNormalizeBossTemplateFile(t *testing.T) {
	got, err := normalizeBossTemplateFile("ArenaBoss")
	if err != nil || got != "ArenaBoss.json" {
		t.Fatalf("normalize file: got %q err=%v", got, err)
	}
	for _, invalid := range []string{"", "../Boss.json", "folder/Boss.json", "Boss?.json"} {
		if _, err := normalizeBossTemplateFile(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestSpreadCoordinate(t *testing.T) {
	x, y := spreadCoordinate(100, 200, 50, 4, 0)
	if x != 150 || y != 200 {
		t.Fatalf("unexpected first point: %v,%v", x, y)
	}
	x, y = spreadCoordinate(100, 200, 0, 4, 1)
	if x != 100 || y != 200 {
		t.Fatalf("zero radius should preserve coordinates: %v,%v", x, y)
	}
}

func TestResponseIndicatesFailure(t *testing.T) {
	cases := []struct {
		response string
		failed   bool
	}{
		{`{"success":true,"error":""}`, false},
		{`{"success":false}`, true},
		{`{"error":"template missing"}`, true},
		{"Unknown command", true},
		{"summoned successfully", false},
	}
	for _, item := range cases {
		if got := responseIndicatesFailure(item.response); got != item.failed {
			t.Fatalf("response %q: got %v want %v", item.response, got, item.failed)
		}
	}
}

func TestExecutorStatusCreatesRequiredDirectories(t *testing.T) {
	root := t.TempDir()
	executor := NewPalDefenderRCONExecutor(PalDefenderRCONOptions{
		Host: "127.0.0.1", Port: 25575, Password: "secret", PalDefenderDir: root,
	})
	status := executor.Status(context.Background())
	if !status.Available {
		t.Fatalf("expected executor to be available: %#v", status)
	}
	for _, relative := range []string{filepath.Join("Pals", "Templates"), filepath.Join("Pals", "Summons")} {
		if err := ensureRegularDirectory(filepath.Join(root, relative)); err != nil {
			t.Fatalf("directory %s: %v", relative, err)
		}
	}
}

func TestExecuteRCON(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	executor := NewPalDefenderRCONExecutor(PalDefenderRCONOptions{
		Host: "127.0.0.1", Port: 25575, Password: "secret", Timeout: time.Second,
	})
	executor.dial = func(context.Context, string, string) (net.Conn, error) { return client, nil }

	done := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		auth, err := readRCONPacket(reader)
		if err != nil {
			done <- err
			return
		}
		if auth.typ != rconPacketAuth || auth.body != "secret" {
			done <- &testProtocolError{"unexpected authentication packet"}
			return
		}
		if err := writeRCONPacket(server, auth.id, rconPacketAuthResponse, ""); err != nil {
			done <- err
			return
		}
		command, err := readRCONPacket(reader)
		if err != nil {
			done <- err
			return
		}
		if command.typ != rconPacketExecCommand || command.body != "/summon Demo" {
			done <- &testProtocolError{"unexpected execution packet"}
			return
		}
		if err := writeRCONPacket(server, command.id, rconPacketResponseValue, "summoned successfully"); err != nil {
			done <- err
			return
		}
		done <- nil
	}()

	response, sent, err := executor.executeRCON(context.Background(), "/summon Demo")
	if err != nil {
		t.Fatalf("execute RCON: %v", err)
	}
	if !sent || !strings.Contains(response, "summoned successfully") {
		t.Fatalf("unexpected result sent=%v response=%q", sent, response)
	}
	if err := <-done; err != nil {
		t.Fatalf("server protocol: %v", err)
	}
}

type testProtocolError struct{ message string }

func (e *testProtocolError) Error() string { return e.message }

func TestExecuteWaveCreatesPalSummon(t *testing.T) {
	root := t.TempDir()
	client, server := net.Pipe()
	defer server.Close()
	executor := NewPalDefenderRCONExecutor(PalDefenderRCONOptions{
		Host: "127.0.0.1", Port: 25575, Password: "secret", PalDefenderDir: root, Timeout: time.Second,
	})
	executor.dial = func(context.Context, string, string) (net.Conn, error) { return client, nil }

	done := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		auth, err := readRCONPacket(reader)
		if err != nil {
			done <- err
			return
		}
		if err := writeRCONPacket(server, auth.id, rconPacketAuthResponse, ""); err != nil {
			done <- err
			return
		}
		command, err := readRCONPacket(reader)
		if err != nil {
			done <- err
			return
		}
		name := strings.TrimSpace(strings.TrimPrefix(command.body, "/summon ")) + ".json"
		payload, err := os.ReadFile(filepath.Join(root, "Pals", "Summons", name))
		if err != nil {
			done <- err
			return
		}
		var document map[string]any
		if err := json.Unmarshal(payload, &document); err != nil {
			done <- err
			return
		}
		if document["PalTemplate"] == "" || document["Uncapturable"] != true || document["X"] != float64(10) || document["Y"] != float64(20) || document["Z"] != float64(30) {
			done <- &testProtocolError{"unexpected PalSummon document"}
			return
		}
		if err := writeRCONPacket(server, command.id, rconPacketResponseValue, "summoned successfully"); err != nil {
			done <- err
			return
		}
		done <- nil
	}()

	result, err := executor.ExecuteWave(context.Background(), Summon{
		ID: "summon_test", Location: Location{X: 10, Y: 20, Z: 30}, Metadata: map[string]any{},
	}, SummonWave{
		Position: 1, Name: "测试Boss", PalID: "SheepBall", Level: 50, Count: 1,
		HPMultiplier: 1, AttackMultiplier: 1, DefenseMultiplier: 1, Capturable: false,
		Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("execute wave: %v", err)
	}
	if result.CompletedCommands != 1 || len(result.Commands) != 1 {
		t.Fatalf("unexpected execution result: %#v", result)
	}
	if err := <-done; err != nil {
		t.Fatalf("server protocol: %v", err)
	}
}

func TestExecuteWaveBlocksUnrepresentedMultipliers(t *testing.T) {
	root := t.TempDir()
	executor := NewPalDefenderRCONExecutor(PalDefenderRCONOptions{
		Host: "127.0.0.1", Port: 25575, Password: "secret", PalDefenderDir: root,
	})
	_, err := executor.ExecuteWave(context.Background(), Summon{
		ID: "summon_test", Location: Location{}, Metadata: map[string]any{},
	}, SummonWave{
		Position: 1, Name: "测试Boss", PalID: "SheepBall", Level: 50, Count: 1,
		HPMultiplier: 10, AttackMultiplier: 1, DefenseMultiplier: 1, Metadata: map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "pal_template_file") {
		t.Fatalf("expected safe multiplier rejection, got %v", err)
	}
}

func TestExecutorStatusReportsDisabledRCON(t *testing.T) {
	disabled := false
	executor := NewPalDefenderRCONExecutor(PalDefenderRCONOptions{
		Host: "127.0.0.1", Port: 25575, Password: "secret", RCONEnabled: &disabled, PalDefenderDir: t.TempDir(),
	})
	status := executor.Status(context.Background())
	if status.Available || status.State != "rcon_disabled" {
		t.Fatalf("unexpected disabled status: %#v", status)
	}
}

func TestInspectBossTemplateFileRejectsMismatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Boss.json")
	if err := os.WriteFile(path, []byte(`{"PalID":"SheepBall","Level":50}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectBossTemplateFile(path, "SheepBall", 50); err != nil {
		t.Fatalf("expected matching template: %v", err)
	}
	if _, err := inspectBossTemplateFile(path, "PinkCat", 50); err == nil {
		t.Fatal("expected PalID mismatch")
	}
	if _, err := inspectBossTemplateFile(path, "SheepBall", 51); err == nil {
		t.Fatal("expected level mismatch")
	}
}
