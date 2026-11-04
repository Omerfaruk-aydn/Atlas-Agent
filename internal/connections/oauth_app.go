package connections

import (
	"os"
	"strings"
)

// oauthApp is the Atlas sign-in application for one provider. A saved
// credential wins. Otherwise the install can supply it once through the
// environment, so the Dots screen can open the provider login without
// asking the person to paste a client id.
func (s *Service) oauthApp(id string) (clientID, secret string) {
	switch id {
	case "google":
		return s.storedOrEnv(googleClientID, "ATLAS_OAUTH_GOOGLE_CLIENT_ID"), s.storedOrEnv(googleSecret, "ATLAS_OAUTH_GOOGLE_CLIENT_SECRET")
	case "outlook":
		return s.storedOrEnv(outlookClientID, "ATLAS_OAUTH_OUTLOOK_CLIENT_ID"), s.storedOrEnv(outlookSecret, "ATLAS_OAUTH_OUTLOOK_CLIENT_SECRET")
	case "slack":
		return s.storedOrEnv(slackClientID, "ATLAS_OAUTH_SLACK_CLIENT_ID"), s.storedOrEnv(slackSecret, "ATLAS_OAUTH_SLACK_CLIENT_SECRET")
	case "github":
		return s.storedOrEnv(githubClientID, "ATLAS_OAUTH_GITHUB_CLIENT_ID"), s.storedOrEnv(githubSecret, "ATLAS_OAUTH_GITHUB_CLIENT_SECRET")
	case "notion":
		return s.storedOrEnv(notionClientID, "ATLAS_OAUTH_NOTION_CLIENT_ID"), s.storedOrEnv(notionSecret, "ATLAS_OAUTH_NOTION_CLIENT_SECRET")
	case "discord":
		return s.storedOrEnv(discordClientID, "ATLAS_OAUTH_DISCORD_CLIENT_ID"), s.storedOrEnv(discordSecret, "ATLAS_OAUTH_DISCORD_CLIENT_SECRET")
	default:
		return "", ""
	}
}

func (s *Service) storedOrEnv(name, env string) string {
	if v, err := s.secrets().Get(name); err == nil && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(os.Getenv(env))
}
