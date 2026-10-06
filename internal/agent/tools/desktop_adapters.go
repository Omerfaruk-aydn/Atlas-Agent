package tools

import "strings"

// DesktopApplicationAdapter describes verified identities and standard UI methods.
// It contains no persisted handles, credentials or task-specific results.
type DesktopApplicationAdapter struct {
	ID           string   `json:"id"`
	Aliases      []string `json:"aliases"`
	Methods      []string `json:"methods"`
	Verification []string `json:"verification"`
}

func desktopApplicationAdapters() []DesktopApplicationAdapter {
	return []DesktopApplicationAdapter{
		{ID: "explorer", Aliases: []string{"File Explorer", "Explorer", "Windows Explorer", "Dosya Gezgini", "Datei-Explorer", "Explorateur de fichiers", "Esplora file", "Explorador de archivos"}, Methods: []string{"fresh file-list target before Ctrl+Shift+N or F2", "Alt+Enter properties with verified helper ownership", "reuse verified explorer_location.path without address shortcuts; otherwise one resolved address Value/Text read"}, Verification: []string{"item name", "file type", "actual folder identity"}},
		{ID: "notepad", Aliases: []string{"Notepad", "Not Defteri", "Editor", "Bloc-notes", "Blocco note"}, Methods: []string{"fresh editable control", "known text and Ctrl+S group", "owned Save dialog then parent readback"}, Verification: []string{"saved document title", "bounded document content", "dialog disappearance alone is insufficient"}},
		{ID: "calculator", Aliases: []string{"Calculator", "Hesap Makinesi", "Rechner", "Calculatrice", "Calcolatrice", "الحاسبة"}, Methods: []string{"observed CalculatorResults selector", "keyboard expression with per-result checkpoint"}, Verification: []string{"actual displayed result for every requested operation"}},
	}
}

func desktopAdapterForWindow(w desktopWindowInfo) DesktopApplicationAdapter {
	for _, adapter := range desktopApplicationAdapters() {
		match := false
		switch adapter.ID {
		case "explorer":
			match = strings.EqualFold(w.ProcessName, "explorer.exe") && w.WindowClass == "CabinetWClass" && (w.Owner == "" || w.Owner == "0")
		case "notepad":
			match = strings.EqualFold(w.ProcessName, "notepad.exe") && (w.Owner == "" || w.Owner == "0")
		case "calculator":
			for _, alias := range adapter.Aliases {
				if strings.EqualFold(w.Name, alias) && (strings.EqualFold(w.ProcessName, "CalculatorApp.exe") || strings.EqualFold(w.ProcessName, "Calculator.exe") || strings.EqualFold(w.ProcessName, "ApplicationFrameHost.exe")) {
					match = true
				}
			}
		}
		if match {
			return adapter
		}
	}
	return DesktopApplicationAdapter{ID: "generic", Methods: []string{"fresh observed selectors and supported patterns", "verified Button/Edit alternatives when semantic pattern is absent"}, Verification: []string{"explicit expected-state checkpoint"}}
}
