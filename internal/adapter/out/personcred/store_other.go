//go:build !darwin

package personcred

func platformStore() Store { return FileStore{} }
