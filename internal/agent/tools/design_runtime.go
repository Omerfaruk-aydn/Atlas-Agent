	return taskstate.WriteJSONAtomic(path, value)
}

func writeDesignArtifact(root, rel string, data []byte) error {
	path, err := transfer.Resolve(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return taskstate.WriteFileAtomic(path, data, 0o600)
}
