package proto

// DirectoryEntry is one child of a listed workspace directory. Path is
// relative to the workspace root and uses forward slashes.
type DirectoryEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	IsDir      bool   `json:"is_dir"`
	Size       int64  `json:"size,omitempty"`
	ModifiedAt int64  `json:"modified_at,omitempty"`
}

// DirectoryListing is one level of a workspace directory.
type DirectoryListing struct {
	Path      string           `json:"path"`
	Entries   []DirectoryEntry `json:"entries"`
	Truncated bool             `json:"truncated,omitempty"`
}

// FileContent is a read-only view of a workspace file. Content is empty
// for binary files. SHA256 covers the whole file on disk, not only the
// returned prefix, so a client can detect a change made elsewhere.
type FileContent struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Size       int64  `json:"size"`
	ModifiedAt int64  `json:"modified_at"`
	SHA256     string `json:"sha256"`
	Truncated  bool   `json:"truncated,omitempty"`
	Binary     bool   `json:"binary,omitempty"`
}

// PermissionModeRequest gets or sets a workspace's permission mode:
// "manual", "auto_accept_edits", "plan" or "bypass".
type PermissionModeRequest struct {
	Mode string `json:"mode"`
}

// QuestionCancel names the question batch to cancel. An empty ID cancels
// the batch currently displayed, matching the older bodiless request.
type QuestionCancel struct {
	BatchRequestID string `json:"batch_request_id,omitempty"`
}

// SessionRename is a request to change only a session's title.
type SessionRename struct {
	Title string `json:"title"`
}
