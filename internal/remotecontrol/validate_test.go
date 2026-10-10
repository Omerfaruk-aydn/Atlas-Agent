package remotecontrol

import (
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func validDraft(t *testing.T) Draft {
	t.Helper()
	return Draft{
		ID: NewProfileID(), Name: "Hetzner", SSHHost: "203.0.113.10", SSHPort: "22", SSHUser: "ubuntu",
		SSHAuth: AuthKey, SSHKeyFile: filepath.Join(t.TempDir(), "id_ed25519"), PublicHost: "atlas.example.com",
	}
}

func TestSSHHostDistinguishesAddressesNamesAndURLs(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"203.0.113.10":          "203.0.113.10",
		" 2001:db8::1 ":         "2001:db8::1",
		"[2001:db8::1]":         "2001:db8::1",
		"::ffff:203.0.113.10":   "203.0.113.10",
		"VPS.Example.COM.":      "vps.example.com",
		"bücher.example":        "xn--bcher-kva.example",
		"server-1":              "server-1",
		"xn--bcher-kva.example": "xn--bcher-kva.example",
	} {
		got, msg := NormalizeSSHHost(in)
		require.True(t, msg.IsZero(), "%q: %s", in, msg)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{
		"", "ssh://203.0.113.10", "https://vps.example.com", "root@203.0.113.10", "203.0.113.10:22",
		"[2001:db8::1]:22", "vps.example.com:2222", "2001:db8::zz", "fe80::1%eth0", "0.0.0.0", "::",
		"vps.example.com/path", "two words", "-bad.example", "bad_.example", "a..b", "$(reboot)", "`id`", "a;b",
	} {
		_, msg := NormalizeSSHHost(bad)
		require.False(t, msg.IsZero(), "%q must be rejected", bad)
	}
}

func TestPublicHostMustBeADomainNameWithoutSchemeOrPort(t *testing.T) {
	t.Parallel()
	got, msg := NormalizePublicHost("Atlas.Example.com")
	require.True(t, msg.IsZero())
	require.Equal(t, "atlas.example.com", got)
	got, msg = NormalizePublicHost("uzak.bilgisayarım.example")
	require.True(t, msg.IsZero(), msg.String())
	require.Equal(t, "uzak.xn--bilgisayarm-8zb.example", got)
	for _, bad := range []string{"", "https://atlas.example.com", "atlas.example.com:443", "203.0.113.10", "2001:db8::1", "[2001:db8::1]", "localhost", "atlas", "pc.local", "atlas.example.com/", "user@atlas.example.com", "a b.example.com"} {
		_, msg := NormalizePublicHost(bad)
		require.False(t, msg.IsZero(), "%q must be rejected", bad)
	}
}

func TestDraftValidationReportsEveryFieldInPlace(t *testing.T) {
	t.Parallel()
	d := validDraft(t)
	p, errs := d.Validate()
	require.Empty(t, errs)
	require.Equal(t, 22, p.SSHPort)
	require.Equal(t, 15, p.ConnectTimeout)

	d = Draft{SSHAuth: AuthPassword, SSHPort: "70000", Timeout: "1", ACMEEmail: "me@x", SSHUser: "root;rm"}
	_, errs = d.Validate()
	for _, field := range []string{FieldName, FieldSSHHost, FieldSSHPort, FieldSSHUser, FieldSecret, FieldPublicHost, FieldACMEEmail, FieldTimeout} {
		require.Contains(t, errs, field)
	}
	for _, port := range []string{"0", "-1", "abc", "65536", "22.5"} {
		d := validDraft(t)
		d.SSHPort = port
		_, errs := d.Validate()
		require.Contains(t, errs, FieldSSHPort, port)
	}
	d = validDraft(t)
	d.SSHPort = ""
	p, errs = d.Validate()
	require.Empty(t, errs)
	require.Equal(t, 22, p.SSHPort, "the port defaults to 22")

	d = validDraft(t)
	d.SSHKeyFile = "id_ed25519"
	_, errs = d.Validate()
	require.Contains(t, errs, FieldSSHKeyFile, "relative key paths are refused")

	d = validDraft(t)
	d.SSHAuth = AuthPassword
	d.HasSecret = true
	_, errs = d.Validate()
	require.Empty(t, errs, "an empty password keeps the stored one")

	for _, email := range []string{"you@example.com", "a.b+tag@sub.example.org"} {
		d := validDraft(t)
		d.ACMEEmail = email
		p, errs := d.Validate()
		require.Empty(t, errs, email)
		require.Equal(t, email, p.ACMEEmail)
	}
	for _, email := range []string{"you@localhost", "a b@example.com", "x$y@example.com", `"q"@example.com`, "no-at"} {
		d := validDraft(t)
		d.ACMEEmail = email
		_, errs := d.Validate()
		require.Contains(t, errs, FieldACMEEmail, email)
	}
}

func TestDraftRoundTripsThroughProfileDefaults(t *testing.T) {
	t.Parallel()
	d := DraftFromProfile(config.RemoteProfile{ID: "rp-000000000001", Name: "Old", SSHHost: "vps.example.com", SSHUser: "root", PublicHost: "a.example.com"})
	require.Equal(t, "22", d.SSHPort, "older profiles without a port use 22")
	require.Equal(t, "15", d.Timeout)
	require.Equal(t, AuthKey, d.SSHAuth)
	require.True(t, ValidID("rp-000000000001"))
	require.False(t, ValidID("../../etc"))
	require.True(t, ValidID(NewProfileID()))
}

func TestShellCommandQuotesEveryPart(t *testing.T) {
	t.Parallel()
	require.Equal(t, `sh -c 'echo "$1"' sh 'it'\''s'`, ShellCommand(`echo "$1"`, "it's"))
}
