Git adapter docwrite.go — unreachable residue of the knowledge-store deletion
Status: open
Priority: later

[internal/adapters/git/docwrite.go](../../internal/adapters/git/docwrite.go) carries
`DocAuthConfig`, `CommitAndPush`, and `buildDocAuth` with no caller outside the package — dead
residue of the D-0113 knowledge-store deletion. It still declares inline auth fields
(`ssh_key_path`, `http_token` style) that the live git adapter no longer models after D-0150.

Delete the file and its test-only helpers, or record a reason to keep it.
