package engineering

// RecipeBinding ties an admitted recipe to its immutable compilation and graph.
// Prepared bindings block dispatch until the SQLite mutation is reconciled.
type RecipeBinding struct {
	ID                       string `json:"id"`
	SessionID                string `json:"session_id"`
	RecipeID                 string `json:"recipe_id"`
	RecipeHash               string `json:"recipe_hash"`
	ParametersHash           string `json:"parameters_hash"`
	Root                     string `json:"root"`
	Status                   string `json:"status"`
	OriginalTodosFingerprint string `json:"original_todos_fingerprint"`
	Record                   Record `json:"record"`
}
