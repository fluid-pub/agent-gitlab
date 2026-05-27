package gitlab

import (
	"math"
	"strings"
	"testing"
)

func TestNewGitCmd_unknownRunAs(t *testing.T) {
	_, err := newGitCmd("no_such_user_fluid_gitlab_xxxxx", "-C", "/tmp", "version")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "run_as") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewGitCmd_emptyRunAs(t *testing.T) {
	cmd, err := newGitCmd("", "-C", "/tmp", "version")
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Fatal("expected no credential when run_as empty")
	}
}

func TestHTTPSGitRemoteURL_doesNotEmbedToken(t *testing.T) {
	remote := httpsGitRemoteURL("gitlab.com", "constellio/infrastructure")
	if strings.Contains(remote, "@") || strings.Contains(remote, "oauth2") {
		t.Fatalf("remote should not contain credentials: %s", remote)
	}
	if remote != "https://gitlab.com/constellio/infrastructure.git" {
		t.Fatalf("unexpected remote: %s", remote)
	}
}

func TestParseUint32ID_outOfRange(t *testing.T) {
	_, err := parseUint32ID("uid", "4294967296")
	if err == nil {
		t.Fatal("expected out-of-range error")
	}
	if !strings.Contains(err.Error(), "uid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseUint32ID_validMax(t *testing.T) {
	got, err := parseUint32ID("gid", "4294967295")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != math.MaxUint32 {
		t.Fatalf("unexpected gid: %d", got)
	}
}
