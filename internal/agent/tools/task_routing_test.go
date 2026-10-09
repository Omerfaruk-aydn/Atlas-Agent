package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskRoutingPreloadsRelevantCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		prompt string
		want   []string
	}{
		{"youtube dan rraenee i açarmısın en son attığı yeni video yu", []string{"browser", "tool_pipeline"}},
		{"Open https://example.com", []string{"browser", "tool_pipeline"}},
		{"browser use ile aç", []string{"browser", "tool_pipeline"}},
		{"Hesap Makinesi'ni aç", []string{"computer", "tool_pipeline"}},
		{"Not Defteri'ne sonuçları yaz", []string{"computer", "tool_pipeline"}},
		{"Run the tests", []string{"test_run"}},
		{"lint çalıştır", []string{"lint_run"}},
		{"coverage report", []string{"coverage_report"}},
		{"bellekte ara", []string{"memory"}},
		{"Atlas ayarlarını değiştir", []string{"atlas_config"}},
		{"security scan", []string{"security_scan"}},
		{"git durumunu incele", []string{"git_status", "git_diff"}},
		{"bu projenin haritasını çıkar", []string{"project_map"}},
		{"oturum geçmişini bul", []string{"session_search"}},
		{"merhaba", nil},
		{"YouTube API client kodunu düzenle", nil},
		{"Do not open YouTube; explain its API", nil},
	} {
		t.Run(tc.prompt, func(t *testing.T) {
			t.Parallel()
			require.ElementsMatch(t, tc.want, IntentTools(tc.prompt))
		})
	}
}
