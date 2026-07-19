package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jaimegago/joe/internal/crypto"
)

// ComponentConfigScan counts component rows and how many of them carry an
// encrypted config, learned WITHOUT decrypting anything and therefore without
// needing a key.
type ComponentConfigScan struct {
	Total     int
	Encrypted int
}

// ScanComponentConfigs reports how many component rows exist and how many hold
// an encrypted config. It exists so a caller that has no key — the boot key
// loader deciding whether an absent key file is a first run, and `joe db
// restore` inspecting a backup — can tell "nothing to lose" from "everything to
// lose".
//
// The detection MIRRORS the production read path in encrypted_components.go:
// unmarshal the config column as a JSON string first, and only then test the
// result for the encrypted marker. Two storage accidents make the obvious
// shortcuts wrong: the value is stored JSON-QUOTED, and it lands in the column
// with BLOB storage class despite the TEXT declaration, so a SQL LIKE against it
// matches nothing. An unmarshal failure means a plaintext config (the
// repository's backward-compatibility branch), not an error.
//
// It assumes the components table exists, which is true of any caller running
// after migrations. A caller inspecting a FOREIGN file must establish that
// separately — the existence check is driver-specific and this query is not.
func ScanComponentConfigs(ctx context.Context, db *sql.DB) (ComponentConfigScan, error) {
	rows, err := db.QueryContext(ctx, `SELECT config FROM components`)
	if err != nil {
		return ComponentConfigScan{}, err
	}
	defer rows.Close()

	var scan ComponentConfigScan
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return ComponentConfigScan{}, err
		}
		scan.Total++
		var unquoted string
		if err := json.Unmarshal(raw, &unquoted); err != nil {
			continue // not a JSON string: a plaintext config, not an error
		}
		if crypto.IsEncrypted(unquoted) {
			scan.Encrypted++
		}
	}
	return scan, rows.Err()
}
