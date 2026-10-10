package remotecontrol

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"golang.org/x/net/idna"
)

// Authentication methods.
const (
	AuthKey      = "key"
	AuthAgent    = "agent"
	AuthPassword = "password"
)

// Form field names, used as keys of FieldErrors.
const (
	FieldName       = "name"
	FieldSSHHost    = "ssh_host"
	FieldSSHPort    = "ssh_port"
	FieldSSHUser    = "ssh_user"
	FieldSSHAuth    = "ssh_auth"
	FieldSSHKeyFile = "ssh_key_file"
	FieldSecret     = "secret"
	FieldPublicHost = "public_host"
	FieldACMEEmail  = "acme_email"
	FieldTimeout    = "timeout"
)

// FieldErrors maps a form field to what is wrong with it.
type FieldErrors map[string]Message

// Draft is the raw form input. Every field is a string, as typed.
type Draft struct {
	ID, Name, SSHHost, SSHPort, SSHUser, SSHAuth, SSHKeyFile string
	PublicHost, ACMEEmail, Timeout                           string
	IgnoreUserKnownHosts                                     bool
	// HasSecret reports whether a sealed secret already exists, so an empty
	// password field means "keep it".
	HasSecret bool
	// Secret is the password or key passphrase typed in this form. It is
	// never stored in the profile; see Secrets.
	Secret string
}

// DraftFromProfile fills the form from a saved profile.
func DraftFromProfile(p config.RemoteProfile) Draft {
	d := Draft{
		ID: p.ID, Name: p.Name, SSHHost: p.SSHHost, SSHUser: p.SSHUser, SSHAuth: p.SSHAuth,
		SSHKeyFile: p.SSHKeyFile, PublicHost: p.PublicHost, ACMEEmail: p.ACMEEmail,
		IgnoreUserKnownHosts: p.IgnoreUserKnownHosts, HasSecret: p.SecretRef != "",
		SSHPort: strconv.Itoa(p.Port()), Timeout: strconv.Itoa(p.Timeout()),
	}
	if d.SSHAuth == "" {
		d.SSHAuth = AuthKey
	}
	return d
}

var userPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)

// emailPattern is deliberately narrower than RFC 5322: the address is
// written to a Compose environment file, where quotes, spaces and '$' have
// meaning.
var emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+-]{1,64}@[^@]+$`)

// Validate checks the form without touching the network or disk and returns
// the normalized profile. Errors are reported per field.
func (d Draft) Validate() (config.RemoteProfile, FieldErrors) {
	errs := FieldErrors{}
	p := config.RemoteProfile{ID: d.ID, SSHAuth: d.SSHAuth, IgnoreUserKnownHosts: d.IgnoreUserKnownHosts}

	p.Name = strings.TrimSpace(d.Name)
	switch {
	case p.Name == "":
		errs[FieldName] = Msg("Enter a profile name.")
	case len([]rune(p.Name)) > 48 || strings.ContainsFunc(p.Name, unicode.IsControl):
		errs[FieldName] = Msg("Use at most 48 printable characters.")
	}

	if host, msg := NormalizeSSHHost(d.SSHHost); msg.IsZero() {
		p.SSHHost = host
	} else {
		errs[FieldSSHHost] = msg
	}

	if port, msg := parseRange(d.SSHPort, 22, 1, 65535); msg.IsZero() {
		p.SSHPort = port
	} else {
		errs[FieldSSHPort] = msg
	}

	p.SSHUser = strings.TrimSpace(d.SSHUser)
	switch {
	case p.SSHUser == "":
		errs[FieldSSHUser] = Msg("Enter the SSH user name.")
	case !userPattern.MatchString(p.SSHUser):
		errs[FieldSSHUser] = Msg("Use a Linux user name: letters, digits, '.', '_' or '-'.")
	}

	switch d.SSHAuth {
	case AuthKey:
		p.SSHKeyFile = strings.TrimSpace(d.SSHKeyFile)
		switch {
		case p.SSHKeyFile == "":
			errs[FieldSSHKeyFile] = Msg("Enter the path of the private key file.")
		case !filepath.IsAbs(p.SSHKeyFile):
			errs[FieldSSHKeyFile] = Msg("Use an absolute path.")
		}
	case AuthAgent:
	case AuthPassword:
		if d.Secret == "" && !d.HasSecret {
			errs[FieldSecret] = Msg("Enter the SSH password.")
		}
	default:
		errs[FieldSSHAuth] = Msg("Choose key, agent or password.")
	}
	if len(d.Secret) > 4096 {
		errs[FieldSecret] = Msg("The secret is too long.")
	}

	if host, msg := NormalizePublicHost(d.PublicHost); msg.IsZero() {
		p.PublicHost = host
	} else {
		errs[FieldPublicHost] = msg
	}

	if email := strings.TrimSpace(d.ACMEEmail); email != "" {
		at := strings.LastIndexByte(email, '@')
		if !emailPattern.MatchString(email) || at < 0 {
			errs[FieldACMEEmail] = Msg("Enter a plain e-mail address such as you@example.com.")
		} else if domain, msg := normalizeHostname(email[at+1:]); !msg.IsZero() || !strings.Contains(domain, ".") {
			errs[FieldACMEEmail] = Msg("Enter a plain e-mail address such as you@example.com.")
		} else {
			p.ACMEEmail = email[:at+1] + domain
		}
	}

	if timeout, msg := parseRange(d.Timeout, 15, 3, 120); msg.IsZero() {
		p.ConnectTimeout = timeout
	} else {
		errs[FieldTimeout] = msg
	}
	if len(errs) == 0 {
		return p, nil
	}
	return p, errs
}

func parseRange(raw string, def, lo, hi int) (int, Message) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, Message{}
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return 0, Msg("Enter a whole number from %d to %d.", lo, hi)
	}
	return n, Message{}
}

// NormalizeSSHHost accepts an IPv4 or IPv6 address (optionally in brackets)
// or a hostname, including internationalized names, which are converted to
// their ASCII form. URLs, ports, users and paths are rejected with a hint.
func NormalizeSSHHost(raw string) (string, Message) {
	h := strings.TrimSpace(raw)
	switch {
	case h == "":
		return "", Msg("Enter the server IP address or hostname.")
	case strings.Contains(h, "://"):
		return "", Msg("Enter only the address, without a scheme such as ssh:// or https://.")
	case strings.Contains(h, "@"):
		return "", Msg("Put the user name in the user field, not in the address.")
	case strings.ContainsAny(h, "/\\ \t?#"):
		return "", Msg("Enter only the address, without spaces or paths.")
	}
	if strings.HasPrefix(h, "[") {
		if !strings.HasSuffix(h, "]") {
			return "", Msg("Put the port in the port field.")
		}
		h = h[1 : len(h)-1]
		if a, err := netip.ParseAddr(h); err != nil || !a.Is6() {
			return "", Msg("Brackets are only for IPv6 addresses.")
		}
	}
	if a, err := netip.ParseAddr(h); err == nil {
		if a.Zone() != "" {
			return "", Msg("IPv6 zone identifiers are not supported.")
		}
		if a.IsUnspecified() || a.IsMulticast() {
			return "", Msg("This is not a server address.")
		}
		return a.Unmap().String(), Message{}
	}
	if strings.Count(h, ":") == 1 {
		return "", Msg("Put the port in the port field.")
	}
	if strings.Contains(h, ":") {
		return "", Msg("This is not a valid IPv6 address.")
	}
	return normalizeHostname(h)
}

// NormalizePublicHost accepts only a DNS name with at least two labels: the
// certificate authority issues certificates for public names, not for IP
// addresses or single-label names.
func NormalizePublicHost(raw string) (string, Message) {
	h := strings.TrimSpace(raw)
	switch {
	case h == "":
		return "", Msg("Enter the hostname phones will open, such as atlas.example.com.")
	case strings.Contains(h, "://"):
		return "", Msg("Enter the hostname only, without https://.")
	case strings.ContainsAny(h, "/\\ \t?#@"):
		return "", Msg("Enter the hostname only, without a path or user.")
	case strings.Contains(h, ":"):
		if _, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
			return "", Msg("Use a DNS name; certificates are not issued for IP addresses here.")
		}
		return "", Msg("Phones always use port 443; enter the hostname without a port.")
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return "", Msg("Use a DNS name; certificates are not issued for IP addresses here.")
	}
	host, msg := normalizeHostname(h)
	if !msg.IsZero() {
		return "", msg
	}
	if !strings.Contains(host, ".") || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return "", Msg("Use a public DNS name with a domain, such as atlas.example.com.")
	}
	return host, Message{}
}

var lookup = idna.New(idna.MapForLookup(), idna.ValidateLabels(true), idna.StrictDomainName(true), idna.VerifyDNSLength(true), idna.BidiRule())

func normalizeHostname(h string) (string, Message) {
	h = strings.TrimSuffix(h, ".")
	ascii, err := lookup.ToASCII(h)
	if err != nil || ascii == "" {
		return "", Msg("This is not a valid hostname.")
	}
	ascii = strings.ToLower(ascii)
	for label := range strings.SplitSeq(ascii, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", Msg("This is not a valid hostname.")
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return "", Msg("This is not a valid hostname.")
			}
		}
	}
	if len(ascii) > 253 {
		return "", Msg("This is not a valid hostname.")
	}
	return ascii, Message{}
}

// CheckKeyFile verifies that a private key file exists and is a small
// regular file. It does not read the key.
func CheckKeyFile(path string) Message {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return Msg("The key file does not exist.")
	case err != nil:
		return Msg("The key file cannot be read.")
	case !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 64<<10:
		return Msg("This does not look like a private key file.")
	}
	return Message{}
}

// NewProfileID returns a random profile identifier.
func NewProfileID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "rp-" + hex.EncodeToString(b[:])
}

var idPattern = regexp.MustCompile(`^rp-[0-9a-f]{12}$`)

// ValidID reports whether id is a profile identifier this package created.
// IDs name local files, so anything else is refused.
func ValidID(id string) bool { return idPattern.MatchString(id) }
