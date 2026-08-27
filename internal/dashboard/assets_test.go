package dashboard

import (
	"io/fs"
	"testing"
)

func TestEmbeddedAssetsStayUnderBudget(t *testing.T) {
	t.Parallel()

	const maximumBytes = 100_000
	var total int64
	err := fs.WalkDir(assets, "assets", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		t.Logf("%s: %d bytes", path, info.Size())
		return nil
	})
	if err != nil {
		t.Fatalf("measure embedded dashboard assets: %v", err)
	}
	if total >= maximumBytes {
		t.Fatalf("embedded dashboard assets total %d bytes, must stay below %d", total, maximumBytes)
	}
	t.Logf("dashboard asset total: %d/%d bytes", total, maximumBytes)
}
