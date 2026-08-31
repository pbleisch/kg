package batch

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ledgerDir is where applied idempotency keys are recorded.  It sits under the
// existing derived-data directory, not beside the entities: it is a record of
// what happened, not knowledge.
func ledgerDir(kgDir string) string { return filepath.Join(kgDir, ".cache", "apply") }

func ledgerPath(kgDir, key string) string {
	return filepath.Join(ledgerDir(kgDir), key+".json")
}

// readLedger returns the recorded result for key, or nil if the batch has not
// been applied.  A ledger that cannot be read is treated as absent: a failure
// to remember must not become a failure to write.
func readLedger(kgDir, key string) *Result {
	data, err := os.ReadFile(ledgerPath(kgDir, key))
	if err != nil {
		return nil
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil
	}
	return &r
}

// writeLedger records that key was applied.  Best effort by the same argument
// as above, so its error is returned for reporting and never aborts a batch
// whose writes already landed.
func writeLedger(kgDir, key string, r *Result) error {
	if err := os.MkdirAll(ledgerDir(kgDir), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	path := ledgerPath(kgDir, key)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
