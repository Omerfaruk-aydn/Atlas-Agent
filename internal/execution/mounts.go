package execution

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// Mask recognized credential files and private tool metadata inside the project.
func (r *ociRunner) privateMounts(ctx context.Context, root string) ([]string, engineering.ArtifactRef, error) {
	mask, err := r.store.PutArtifact(ctx, "execution-mask", nil)
	if err != nil {
		return nil, mask, err
	}
	maskPath := filepath.Join(r.store.Dir(), "artifacts", mask.Hash+".blob")
	if strings.ContainsAny(maskPath, ",\r\n") {
		return nil, mask, fmt.Errorf("execution mask path is not a literal mount")
	}
	args := []string{}
	visits, masks := 0, 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		visits++
		if visits > 50000 {
			return fmt.Errorf("credential mount inspection exceeds visit limit")
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.ContainsAny(relative, ",\r\n") {
			return fmt.Errorf("project path cannot be represented as a literal mount")
		}
		name := strings.ToLower(entry.Name())
		private := false
		switch name {
		case ".git", ".atlas", ".codex", ".claude", ".aws", ".ssh", ".gnupg", "atlas.json", "atlasrc", "credentials", "credentials.json", "credentials.toml", "id_rsa", "id_ed25519":
			private = true
		}
		private = private || strings.HasPrefix(name, ".env") && !strings.HasSuffix(name, ".example") || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key")
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("unresolved project symlink")
			}
			contained, err := filepath.Rel(root, target)
			if err != nil || contained == ".." || filepath.IsAbs(contained) || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
				return fmt.Errorf("project symlink escapes the execution mount")
			}
		}
		if !private {
			return nil
		}
		masks++
		if masks > 128 {
			return fmt.Errorf("credential mount mask count exceeds limit")
		}
		target := "/workspace/" + filepath.ToSlash(relative)
		if entry.IsDir() {
			args = append(args, "--tmpfs", target+":ro,nosuid,nodev,noexec,size=4096")
			return fs.SkipDir
		}
		args = append(args, "--mount", "type=bind,source="+maskPath+",target="+target+",readonly")
		return nil
	})
	return args, mask, err
}
